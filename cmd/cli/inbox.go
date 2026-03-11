package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func inboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "View and manage inbox notifications",
	}

	cmd.AddCommand(inboxListCmd())
	cmd.AddCommand(inboxReadCmd())

	return cmd
}

func inboxListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List unread inbox items",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var items []map[string]any
			if err := c.get("/api/inbox", &items); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(items)
			}

			if len(items) == 0 {
				fmt.Println("No unread inbox items.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tKIND\tTITLE\tCREATED")
			for _, item := range items {
				id, _ := item["id"].(string)
				kind, _ := item["kind"].(string)
				title, _ := item["title"].(string)
				created, _ := item["created_at"].(string)
				if len(id) > 8 {
					id = id[:8]
				}
				if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
					created = t.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, kind, title, created)
			}
			return w.Flush()
		},
	}
}

func inboxReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read <id>",
		Short: "Mark an inbox item as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.post("/api/inbox/"+args[0]+"/read", nil, nil); err != nil {
				return err
			}
			fmt.Println("Marked as read.")
			return nil
		},
	}
}
