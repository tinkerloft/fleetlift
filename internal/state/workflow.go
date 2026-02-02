// Package state manages CLI state like last workflow ID.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	stateDir         = ".fleetlift"
	lastWorkflowFile = "last-workflow"
)

// getStatePath returns the path to the state directory.
func getStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, stateDir), nil
}

// SaveLastWorkflow saves the workflow ID to ~/.fleetlift/last-workflow.
func SaveLastWorkflow(workflowID string) error {
	statePath, err := getStatePath()
	if err != nil {
		return err
	}

	// Create state directory if it doesn't exist
	if err := os.MkdirAll(statePath, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	filePath := filepath.Join(statePath, lastWorkflowFile)
	if err := os.WriteFile(filePath, []byte(workflowID+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to save last workflow: %w", err)
	}

	return nil
}

// GetLastWorkflow reads the last workflow ID from ~/.fleetlift/last-workflow.
func GetLastWorkflow() (string, error) {
	statePath, err := getStatePath()
	if err != nil {
		return "", err
	}

	filePath := filepath.Join(statePath, lastWorkflowFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no previous workflow found")
		}
		return "", fmt.Errorf("failed to read last workflow: %w", err)
	}

	workflowID := strings.TrimSpace(string(data))
	if workflowID == "" {
		return "", fmt.Errorf("last workflow file is empty")
	}

	return workflowID, nil
}
