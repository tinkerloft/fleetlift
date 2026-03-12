package agent

import "strings"

// shellQuote wraps s in single quotes, escaping any single quotes within.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}
