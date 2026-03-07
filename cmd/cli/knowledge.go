package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/tinkerloft/fleetlift/internal/knowledge"
	"github.com/tinkerloft/fleetlift/internal/model"
)

var knowledgeCmd = &cobra.Command{
	Use:   "knowledge",
	Short: "Manage knowledge items from past transformations",
}

var knowledgeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List knowledge items",
	RunE:  runKnowledgeList,
}

var knowledgeShowCmd = &cobra.Command{
	Use:   "show <item-id>",
	Short: "Show a knowledge item in detail",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeShow,
}

var knowledgeAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Manually add a knowledge item",
	RunE:  runKnowledgeAdd,
}

var knowledgeDeleteCmd = &cobra.Command{
	Use:   "delete <item-id>",
	Short: "Delete a knowledge item",
	Args:  cobra.ExactArgs(1),
	RunE:  runKnowledgeDelete,
}

var knowledgeReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Interactively review pending knowledge items (approve/edit/delete)",
	Long: `Review auto-captured knowledge items one by one.
For each item: [a]pprove promotes it, [d]elete removes it, [e]dit lets you update the summary before approving, [s]kip leaves it as pending.

After review, run 'fleetlift knowledge commit --repo <path>' to copy approved items to a repo.`,
	RunE: runKnowledgeReview,
}

func runKnowledgeReview(cmd *cobra.Command, args []string) error {
	taskID, _ := cmd.Flags().GetString("task-id")
	store := knowledge.DefaultStore()

	var items []model.KnowledgeItem
	var err error
	if taskID != "" {
		items, err = store.List(taskID)
	} else {
		items, err = store.ListAll()
	}
	if err != nil {
		return err
	}

	// Filter to pending items only (Status == "" or "pending")
	var pending []model.KnowledgeItem
	for _, item := range items {
		if item.Status == "" || item.Status == model.KnowledgeStatusPending {
			pending = append(pending, item)
		}
	}

	if len(pending) == 0 {
		fmt.Println("No pending knowledge items to review.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	approved, deleted, skipped := 0, 0, 0

	for i, item := range pending {
		fmt.Printf("\n--- Item %d/%d ---\n", i+1, len(pending))
		fmt.Printf("ID:         %s\n", item.ID)
		fmt.Printf("Type:       %s\n", item.Type)
		fmt.Printf("Confidence: %.2f\n", item.Confidence)
		if len(item.Tags) > 0 {
			fmt.Printf("Tags:       %s\n", strings.Join(item.Tags, ", "))
		}
		fmt.Printf("Summary:    %s\n", item.Summary)
		if item.Details != "" {
			fmt.Printf("Details:    %s\n", item.Details)
		}
		if item.CreatedFrom != nil && item.CreatedFrom.TaskID != "" {
			fmt.Printf("From task:  %s\n", item.CreatedFrom.TaskID)
		}

	prompt:
		fmt.Print("\n[a]pprove / [d]elete / [s]kip / [e]dit summary: ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))

		switch line {
		case "a", "approve":
			item.Status = model.KnowledgeStatusApproved
			if err := store.Update(item); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to approve %s: %v\n", item.ID, err)
			} else {
				approved++
			}
		case "d", "delete":
			if err := store.Delete(item.ID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete %s: %v\n", item.ID, err)
			} else {
				deleted++
			}
		case "s", "skip", "":
			skipped++
		case "e", "edit":
			fmt.Printf("New summary (leave blank to cancel): ")
			newSummary, _ := reader.ReadString('\n')
			newSummary = strings.TrimSpace(newSummary)
			if newSummary == "" {
				goto prompt
			}
			item.Summary = newSummary
			item.Status = model.KnowledgeStatusApproved
			if err := store.Update(item); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update %s: %v\n", item.ID, err)
			} else {
				approved++
			}
		default:
			fmt.Println("Please enter a, d, s, or e.")
			goto prompt
		}
	}

	fmt.Printf("\nReview complete: %d approved, %d deleted, %d skipped\n", approved, deleted, skipped)
	if approved > 0 {
		fmt.Println("Run 'fleetlift knowledge commit --repo <path>' to promote approved items to a repo.")
	}
	return nil
}

func init() {
	knowledgeListCmd.Flags().String("task-id", "", "Filter by task ID")
	knowledgeListCmd.Flags().String("type", "", "Filter by type (pattern|correction|gotcha|context)")
	knowledgeListCmd.Flags().String("tag", "", "Filter by tag")

	knowledgeAddCmd.Flags().String("summary", "", "One-line summary (required)")
	knowledgeAddCmd.Flags().String("type", "pattern", "Type: pattern|correction|gotcha|context")
	knowledgeAddCmd.Flags().String("details", "", "Longer explanation")
	knowledgeAddCmd.Flags().StringSlice("tags", nil, "Comma-separated tags")
	_ = knowledgeAddCmd.MarkFlagRequired("summary")

	knowledgeCmd.AddCommand(knowledgeListCmd, knowledgeShowCmd, knowledgeAddCmd, knowledgeDeleteCmd)
	knowledgeCmd.AddCommand(knowledgeReviewCmd)
	knowledgeReviewCmd.Flags().String("task-id", "", "Filter review to a specific task ID")
}

func runKnowledgeList(cmd *cobra.Command, _ []string) error {
	taskID, _ := cmd.Flags().GetString("task-id")
	typeFilter, _ := cmd.Flags().GetString("type")
	tagFilter, _ := cmd.Flags().GetString("tag")

	store := knowledge.DefaultStore()

	var items []model.KnowledgeItem
	var err error
	if taskID != "" {
		items, err = store.List(taskID)
	} else {
		items, err = store.ListAll()
	}
	if err != nil {
		return fmt.Errorf("listing knowledge items: %w", err)
	}

	// Apply filters.
	items = filterKnowledgeItems(items, typeFilter, tagFilter)

	if len(items) == 0 {
		fmt.Println("No knowledge items found.")
		return nil
	}

	fmt.Print(formatKnowledgeTable(items))
	return nil
}

func runKnowledgeShow(_ *cobra.Command, args []string) error {
	itemID := args[0]
	store := knowledge.DefaultStore()

	all, err := store.ListAll()
	if err != nil {
		return err
	}
	for _, item := range all {
		if item.ID == itemID {
			fmt.Printf("ID:         %s\n", item.ID)
			fmt.Printf("Type:       %s\n", item.Type)
			fmt.Printf("Source:     %s\n", item.Source)
			fmt.Printf("Confidence: %.2f\n", item.Confidence)
			fmt.Printf("Tags:       %s\n", strings.Join(item.Tags, ", "))
			fmt.Printf("Created:    %s\n", item.CreatedAt.Format(time.RFC3339))
			fmt.Printf("\nSummary:\n  %s\n", item.Summary)
			if item.Details != "" {
				fmt.Printf("\nDetails:\n  %s\n", item.Details)
			}
			if item.CreatedFrom != nil {
				fmt.Printf("\nOrigin:\n  Task: %s\n", item.CreatedFrom.TaskID)
				if item.CreatedFrom.SteeringPrompt != "" {
					fmt.Printf("  Steering: %s\n", item.CreatedFrom.SteeringPrompt)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("knowledge item %q not found", itemID)
}

func runKnowledgeAdd(cmd *cobra.Command, _ []string) error {
	summary, _ := cmd.Flags().GetString("summary")
	typStr, _ := cmd.Flags().GetString("type")
	details, _ := cmd.Flags().GetString("details")
	tags, _ := cmd.Flags().GetStringSlice("tags")

	item := model.KnowledgeItem{
		ID:         uuid.New().String()[:8],
		Type:       model.KnowledgeType(typStr),
		Summary:    summary,
		Details:    details,
		Source:     model.KnowledgeSourceManual,
		Tags:       tags,
		Confidence: 1.0,
		CreatedAt:  time.Now().UTC(),
	}

	store := knowledge.DefaultStore()
	if err := store.Write("manual", item); err != nil {
		return fmt.Errorf("saving knowledge item: %w", err)
	}
	fmt.Printf("Added knowledge item: %s\n", item.ID)
	return nil
}

func runKnowledgeDelete(_ *cobra.Command, args []string) error {
	itemID := args[0]
	store := knowledge.DefaultStore()
	if err := store.Delete(itemID); err != nil {
		return err
	}
	fmt.Printf("Deleted knowledge item: %s\n", itemID)
	return nil
}

// filterKnowledgeItems filters items by type and/or tag.
func filterKnowledgeItems(items []model.KnowledgeItem, typeFilter, tagFilter string) []model.KnowledgeItem {
	if typeFilter == "" && tagFilter == "" {
		return items
	}
	var out []model.KnowledgeItem
	for _, item := range items {
		if typeFilter != "" && string(item.Type) != typeFilter {
			continue
		}
		if tagFilter != "" {
			matched := false
			for _, t := range item.Tags {
				if strings.EqualFold(t, tagFilter) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

// formatKnowledgeTable formats knowledge items as a plain text table.
func formatKnowledgeTable(items []model.KnowledgeItem) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-10s  %-12s  %-6s  %-8s  %s\n", "ID", "TYPE", "CONF", "TAGS", "SUMMARY"))
	sb.WriteString(strings.Repeat("-", 80) + "\n")
	for _, item := range items {
		tags := strings.Join(item.Tags, ",")
		if len(tags) > 8 {
			tags = tags[:7] + "…"
		}
		summary := item.Summary
		if len(summary) > 50 {
			summary = summary[:49] + "…"
		}
		sb.WriteString(fmt.Sprintf("%-10s  %-12s  %.2f    %-8s  %s\n",
			item.ID, item.Type, item.Confidence, tags, summary))
	}
	return sb.String()
}
