// Package shellquote provides shell-safe string quoting.
package shellquote

import "strings"

// Quote wraps s in single quotes, escaping any single quotes within.
func Quote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
