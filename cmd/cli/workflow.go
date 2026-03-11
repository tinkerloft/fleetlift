package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func workflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workflow",
		Aliases: []string{"wf"},
		Short:   "Manage workflow templates",
	}

	cmd.AddCommand(workflowListCmd())
	cmd.AddCommand(workflowGetCmd())
	cmd.AddCommand(workflowCreateCmd())
	cmd.AddCommand(workflowDeleteCmd())
	cmd.AddCommand(workflowForkCmd())

	return cmd
}

func workflowListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available workflow templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var workflows []map[string]any
			if err := c.get("/api/workflows", &workflows); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(workflows)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tTITLE\tBUILTIN\tTAGS")
			for _, wf := range workflows {
				slug, _ := wf["slug"].(string)
				title, _ := wf["title"].(string)
				builtin, _ := wf["builtin"].(bool)
				tags, _ := wf["tags"].([]any)
				tagStr := formatTags(tags)
				bi := ""
				if builtin {
					bi = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", slug, title, bi, tagStr)
			}
			return w.Flush()
		},
	}
}

func workflowGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <slug>",
		Short: "Get workflow template details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var wf map[string]any
			if err := c.get("/api/workflows/"+args[0], &wf); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(wf)
			}

			title, _ := wf["title"].(string)
			desc, _ := wf["description"].(string)
			slug, _ := wf["slug"].(string)
			fmt.Printf("Slug:        %s\n", slug)
			fmt.Printf("Title:       %s\n", title)
			fmt.Printf("Description: %s\n", desc)

			if yamlBody, ok := wf["yaml_body"].(string); ok {
				fmt.Printf("\n--- YAML ---\n%s\n", yamlBody)
			}
			return nil
		},
	}
}

func workflowCreateCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a workflow template from a YAML file",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}

			c := newClient()
			var result map[string]any
			body := map[string]any{
				"yaml_body": string(data),
			}
			if err := c.post("/api/workflows", body, &result); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			fmt.Printf("Created workflow: %v\n", result["slug"])
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "YAML file path (required)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func workflowDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete a workflow template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.delete("/api/workflows/" + args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted workflow: %s\n", args[0])
			return nil
		},
	}
}

func workflowForkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fork <slug>",
		Short: "Fork a builtin workflow template to your team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var result map[string]any
			if err := c.post("/api/workflows/"+args[0]+"/fork", nil, &result); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			fmt.Printf("Forked workflow: %v\n", result["slug"])
			return nil
		},
	}
}

func formatTags(tags []any) string {
	var strs []string
	for _, t := range tags {
		if s, ok := t.(string); ok {
			strs = append(strs, s)
		}
	}
	if len(strs) == 0 {
		return "-"
	}
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
