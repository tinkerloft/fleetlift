// Package activity contains Temporal activity implementations.
package activity

import "strings"

// gitFailed reports whether stderr from a git command indicates a fatal error.
// ExecStream does not propagate exit codes, so we check stderr instead.
func gitFailed(stderr string) bool {
	return strings.Contains(stderr, "fatal:") || strings.Contains(stderr, "error:")
}
