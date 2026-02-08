package agent

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// ValidateManifest validates a task manifest for required fields and security constraints.
// Command content is trusted (manifest authors are privileged) — only structural and path safety validated.
func ValidateManifest(m *protocol.TaskManifest) error {
	if m.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if m.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	if m.Mode != "transform" && m.Mode != "report" {
		return fmt.Errorf("mode must be 'transform' or 'report', got %q", m.Mode)
	}

	// Validate repo names (H1 — path traversal prevention)
	for _, repo := range m.Repositories {
		if err := sanitizeName(repo.Name, "repository"); err != nil {
			return err
		}
	}
	for _, target := range m.Targets {
		if err := sanitizeName(target.Name, "target"); err != nil {
			return err
		}
	}
	if m.Transformation != nil {
		if err := sanitizeName(m.Transformation.Name, "transformation"); err != nil {
			return err
		}
	}
	for _, fe := range m.ForEach {
		if err := sanitizeName(fe.Name, "forEach target"); err != nil {
			return err
		}
	}

	return nil
}

// sanitizeName validates a name used in path construction to prevent path traversal.
func sanitizeName(name, kind string) error {
	if name == "" {
		return fmt.Errorf("%s name is required", kind)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("%s name %q must not contain '/'", kind, name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%s name %q must not contain '..'", kind, name)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("%s name %q must not contain control characters", kind, name)
	}
	return nil
}
