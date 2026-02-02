package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndGetLastWorkflow(t *testing.T) {
	// Use a temp directory for testing
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	workflowID := "transform-test-task-1234567890"

	// Save workflow
	err := SaveLastWorkflow(workflowID)
	if err != nil {
		t.Fatalf("SaveLastWorkflow failed: %v", err)
	}

	// Verify file was created
	expectedPath := filepath.Join(tmpDir, stateDir, lastWorkflowFile)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to exist", expectedPath)
	}

	// Get workflow
	got, err := GetLastWorkflow()
	if err != nil {
		t.Fatalf("GetLastWorkflow failed: %v", err)
	}

	if got != workflowID {
		t.Errorf("GetLastWorkflow = %q, want %q", got, workflowID)
	}
}

func TestGetLastWorkflow_NoFile(t *testing.T) {
	// Use a temp directory with no state file
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := GetLastWorkflow()
	if err == nil {
		t.Error("GetLastWorkflow should return error when no file exists")
	}
}

func TestGetLastWorkflow_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create empty file
	stateDir := filepath.Join(tmpDir, stateDir)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("Failed to create state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, lastWorkflowFile), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	_, err := GetLastWorkflow()
	if err == nil {
		t.Error("GetLastWorkflow should return error for empty file")
	}
}
