// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	agentboxsandbox "github.com/tinkerloft/agentbox/sandbox"
)

// ReportActivities contains activities for report mode operations.
type ReportActivities struct {
	Provider agentboxsandbox.Provider
}

// NewReportActivities creates a new ReportActivities instance.
func NewReportActivities(provider agentboxsandbox.Provider) *ReportActivities {
	return &ReportActivities{Provider: provider}
}

// ValidateSchemaInput contains inputs for schema validation.
type ValidateSchemaInput struct {
	Frontmatter map[string]any
	Schema      string // JSON Schema as string
}

// ValidateSchema validates frontmatter against a JSON Schema.
func (a *ReportActivities) ValidateSchema(_ context.Context, input ValidateSchemaInput) ([]string, error) {
	if input.Schema == "" {
		return nil, nil
	}

	if input.Frontmatter == nil {
		return []string{"frontmatter is required but was not provided"}, nil
	}

	// Compile the schema
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", strings.NewReader(input.Schema)); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	// Validate the frontmatter
	if err := schema.Validate(input.Frontmatter); err != nil {
		validationErr, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return []string{err.Error()}, nil
		}

		// Extract validation errors with field paths
		var errors []string
		extractValidationErrors(validationErr, &errors)
		return errors, nil
	}

	return nil, nil
}

// extractValidationErrors recursively extracts validation error messages.
func extractValidationErrors(err *jsonschema.ValidationError, errors *[]string) {
	if err.Message != "" {
		path := err.InstanceLocation
		if path == "" {
			path = "/"
		}
		*errors = append(*errors, fmt.Sprintf("%s: %s", path, err.Message))
	}
	for _, cause := range err.Causes {
		extractValidationErrors(cause, errors)
	}
}
