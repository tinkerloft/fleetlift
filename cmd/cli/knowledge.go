package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func knowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage knowledge items",
	}
	cmd.AddCommand(knowledgeListCmd(), knowledgeApproveCmd(), knowledgeRejectCmd())
	return cmd
}

func knowledgeListCmd() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List knowledge items",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			path := "/api/knowledge"
			if status != "" {
				path += "?status=" + status
			}
			var items []map[string]any
			if err := c.get(path, &items); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(items)
			}

			if len(items) == 0 {
				fmt.Println("No knowledge items.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tSUMMARY")
			for _, item := range items {
				id, _ := item["id"].(string)
				typ, _ := item["type"].(string)
				st, _ := item["status"].(string)
				summary, _ := item["summary"].(string)
				if len(id) > 8 {
					id = id[:8]
				}
				if len(summary) > 60 {
					summary = summary[:57] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, typ, st, summary)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (pending|approved|rejected)")
	return cmd
}

func knowledgeApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a knowledge item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.patch("/api/knowledge/"+args[0], map[string]string{"status": "approved"}); err != nil {
				return err
			}
			fmt.Println("Knowledge item approved.")
			return nil
		},
	}
}

func knowledgeRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a knowledge item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.patch("/api/knowledge/"+args[0], map[string]string{"status": "rejected"}); err != nil {
				return err
			}
			fmt.Println("Knowledge item rejected.")
			return nil
		},
	}
}
