package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func credentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credential",
		Aliases: []string{"cred"},
		Short:   "Manage team credentials",
	}

	cmd.AddCommand(credentialListCmd())
	cmd.AddCommand(credentialSetCmd())
	cmd.AddCommand(credentialDeleteCmd())

	return cmd
}

func credentialListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List credential names",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var resp struct {
				Items []map[string]any `json:"items"`
			}
			if err := c.get("/api/credentials", &resp); err != nil {
				return err
			}
			creds := resp.Items

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(creds)
			}

			if len(creds) == 0 {
				fmt.Println("No credentials configured.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tUPDATED")
			for _, cr := range creds {
				name, _ := cr["name"].(string)
				updated, _ := cr["updated_at"].(string)
				_, _ = fmt.Fprintf(w, "%s\t%s\n", name, updated)
			}
			return w.Flush()
		},
	}
}

func credentialSetCmd() *cobra.Command {
	var value string
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set a team credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if value == "" {
				fmt.Fprint(os.Stderr, "Enter credential value: ")
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					value = strings.TrimSpace(scanner.Text())
				}
				if value == "" {
					return fmt.Errorf("credential value is required")
				}
			}
			c := newClient()
			if err := c.post("/api/credentials", map[string]string{
				"name":  args[0],
				"value": value,
			}, nil); err != nil {
				return err
			}
			fmt.Printf("Credential %q saved.\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&value, "value", "v", "", "Credential value (reads from stdin if omitted)")
	return cmd
}

func credentialDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a team credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			if err := c.delete("/api/credentials/" + args[0]); err != nil {
				return err
			}
			fmt.Printf("Credential %q deleted.\n", args[0])
			return nil
		},
	}
}
