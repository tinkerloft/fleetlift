package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Manage workflow runs",
	}

	cmd.AddCommand(runStartCmd())
	cmd.AddCommand(runListCmd())
	cmd.AddCommand(runGetCmd())
	cmd.AddCommand(runLogsCmd())
	cmd.AddCommand(runApproveCmd())
	cmd.AddCommand(runRejectCmd())
	cmd.AddCommand(runSteerCmd())
	cmd.AddCommand(runCancelCmd())

	return cmd
}

func runStartCmd() *cobra.Command {
	var params []string
	var follow bool

	cmd := &cobra.Command{
		Use:   "start <workflow-id>",
		Short: "Start a new workflow run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowID := args[0]

			parameters := map[string]any{}
			for _, p := range params {
				key, val := splitParam(p)
				// Try to parse as JSON, fall back to string
				var v any
				if json.Unmarshal([]byte(val), &v) == nil {
					parameters[key] = v
				} else {
					parameters[key] = val
				}
			}

			c := newClient()
			var result map[string]string
			if err := c.post("/api/runs", map[string]any{
				"workflow_id": workflowID,
				"parameters":  parameters,
			}, &result); err != nil {
				return err
			}

			runID := result["id"]

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			fmt.Printf("Run started: %s\n", runID)

			if follow {
				return streamLogs(c, runID)
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Parameter in key=value format")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow logs after starting")
	return cmd
}

func runListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recent runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var resp struct {
				Items []map[string]any `json:"items"`
			}
			if err := c.get("/api/runs", &resp); err != nil {
				return err
			}
			runs := resp.Items

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(runs)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tWORKFLOW\tSTATUS\tCREATED")
			for _, r := range runs {
				id, _ := r["id"].(string)
				wfTitle, _ := r["workflow_title"].(string)
				status, _ := r["status"].(string)
				created, _ := r["created_at"].(string)
				if len(id) > 8 {
					id = id[:8]
				}
				if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
					created = t.Format("2006-01-02 15:04")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, wfTitle, status, created)
			}
			return w.Flush()
		},
	}
}

func runGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get run details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var result map[string]any
			if err := c.get("/api/runs/"+args[0], &result); err != nil {
				return err
			}

			if outputJSON {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			run, _ := result["run"].(map[string]any)
			steps, _ := result["steps"].([]any)

			fmt.Printf("Run:      %v\n", run["id"])
			fmt.Printf("Workflow: %v\n", run["workflow_title"])
			fmt.Printf("Status:   %v\n", run["status"])
			fmt.Println()

			if len(steps) > 0 {
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "STEP\tSTATUS\tPR")
				for _, s := range steps {
					step, _ := s.(map[string]any)
					stepID, _ := step["step_id"].(string)
					status, _ := step["status"].(string)
					pr, _ := step["pr_url"].(string)
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", stepID, status, pr)
				}
				_ = w.Flush()
			}
			return nil
		},
	}
}

func runLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <id>",
		Short: "Stream run logs via SSE",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			return streamLogs(c, args[0])
		},
	}
}

func runApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a paused run step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var result map[string]string
			if err := c.post("/api/runs/"+args[0]+"/approve", nil, &result); err != nil {
				return err
			}
			fmt.Println("Approved.")
			return nil
		},
	}
}

func runRejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a paused run step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var result map[string]string
			if err := c.post("/api/runs/"+args[0]+"/reject", nil, &result); err != nil {
				return err
			}
			fmt.Println("Rejected.")
			return nil
		},
	}
}

func runSteerCmd() *cobra.Command {
	var prompt string
	cmd := &cobra.Command{
		Use:   "steer <id>",
		Short: "Send a steering instruction to a paused run step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var result map[string]string
			if err := c.post("/api/runs/"+args[0]+"/steer", map[string]string{
				"prompt": prompt,
			}, &result); err != nil {
				return err
			}
			fmt.Println("Steering instruction sent.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Steering instruction (required)")
	_ = cmd.MarkFlagRequired("prompt")
	return cmd
}

func runCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a running workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			var result map[string]string
			if err := c.post("/api/runs/"+args[0]+"/cancel", nil, &result); err != nil {
				return err
			}
			fmt.Println("Cancelled.")
			return nil
		},
	}
}

func streamLogs(c *apiClient, runID string) error {
	fmt.Printf("Streaming logs for run %s...\n\n", runID)
	return c.streamSSE("/api/runs/"+runID+"/events", func(eventType, data string) bool {
		if eventType == "status" {
			var status map[string]string
			if json.Unmarshal([]byte(data), &status) == nil {
				s := status["status"]
				fmt.Printf("\n[status: %s]\n", s)
				if s == "complete" || s == "failed" || s == "cancelled" {
					return false
				}
			}
			return true
		}
		// Log line
		var logLine map[string]any
		if json.Unmarshal([]byte(data), &logLine) == nil {
			content, _ := logLine["content"].(string)
			stream, _ := logLine["stream"].(string)
			if stream == "stderr" {
				fmt.Fprintf(os.Stderr, "%s", content)
			} else {
				fmt.Print(content)
			}
		}
		return true
	})
}

func splitParam(s string) (string, string) {
	for i, c := range s {
		if c == '=' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
