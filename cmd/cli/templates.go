package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage task templates",
	Long:  "List and inspect built-in and user-defined task templates",
}

var templatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE:  runTemplatesList,
}

func init() {
	templatesCmd.AddCommand(templatesListCmd)
}

func runTemplatesList(_ *cobra.Command, _ []string) error {
	all := allTemplates()
	if len(all) == 0 {
		fmt.Println("No templates available.")
		return nil
	}

	fmt.Printf("%-25s %s\n", "NAME", "DESCRIPTION")
	fmt.Printf("%-25s %s\n", "----", "-----------")
	for _, t := range all {
		fmt.Printf("%-25s %s\n", t.Name, t.Description)
	}
	fmt.Printf("\nUse: fleetlift create --template <name> [--repo <url>] [--output file.yaml]\n")
	return nil
}

// allTemplates returns built-in templates followed by user-local templates.
func allTemplates() []Template {
	all := make([]Template, len(builtinTemplates))
	copy(all, builtinTemplates)

	user, _ := listUserTemplates()
	all = append(all, user...)
	return all
}
