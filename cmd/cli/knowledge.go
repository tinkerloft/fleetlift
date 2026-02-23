package main

import (
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
		fmt.Fprintln(os.Stderr, "No knowledge items found.")
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
