package main

import (
	"encoding/json"
	"fmt"
	"os"
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
			var creds []map[string]any
			if err := c.get("/api/credentials", &creds); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(creds)
			}

			if len(creds) == 0 {
				fmt.Println("No credentials configured.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tUPDATED")
			for _, cr := range creds {
				name, _ := cr["name"].(string)
				updated, _ := cr["updated_at"].(string)
				fmt.Fprintf(w, "%s\t%s\n", name, updated)
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
	cmd.Flags().StringVarP(&value, "value", "v", "", "Credential value (required)")
	_ = cmd.MarkFlagRequired("value")
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
