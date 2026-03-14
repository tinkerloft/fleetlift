package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	serverURL  string
	outputJSON bool
)

func main() {
	root := &cobra.Command{
		Use:   "fleetlift",
		Short: "Fleetlift CLI — multi-tenant agentic workflow platform",
	}

	root.PersistentFlags().StringVar(&serverURL, "server", envOr("FLEETLIFT_SERVER", "http://localhost:8080"), "Fleetlift server URL")
	root.PersistentFlags().BoolVar(&outputJSON, "output-json", false, "Output in JSON format")

	root.AddCommand(
		authCmd(),
		workflowCmd(),
		runCmd(),
		inboxCmd(),
		credentialCmd(),
		knowledgeCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
