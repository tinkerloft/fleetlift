// Package activity contains Temporal activity implementations.
package activity

import (
	"strings"
)

// shellQuote properly quotes a string for safe use in shell commands.
// Uses single quotes and escapes any single quotes within.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
