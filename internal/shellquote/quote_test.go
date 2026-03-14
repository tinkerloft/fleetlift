package shellquote_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple string", "simple", "'simple'"},
		{"string with spaces", "with spaces", "'with spaces'"},
		{"string with single quote", "it's", "'it'\"'\"'s'"},
		{"string with multiple quotes", "it's a 'test'", "'it'\"'\"'s a '\"'\"'test'\"'\"''"},
		{"empty string", "", "''"},
		{"string with special chars", "hello$world", "'hello$world'"},
		{"string with newline", "hello\nworld", "'hello\nworld'"},
		{"string with backticks", "echo `date`", "'echo `date`'"},
		{"simple filename", "file.txt", "'file.txt'"},
		{"filename with spaces", "my file.txt", "'my file.txt'"},
		{"filename with single quote", "file's.txt", "'file'\"'\"'s.txt'"},
		{"filename with shell metacharacters", "file; rm -rf /", "'file; rm -rf /'"},
		{"filename with command substitution", "$(whoami).txt", "'$(whoami).txt'"},
		{"filename with backticks", "`id`.txt", "'`id`.txt'"},
		{"filename with double quotes", `file"name.txt`, `'file"name.txt'`},
		{"filename with newline", "file\nname.txt", "'file\nname.txt'"},
		{"path with special chars", "../../../etc/passwd", "'../../../etc/passwd'"},
		{"path/to/file", "path/to/file", "'path/to/file'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shellquote.Quote(tt.input), "input: %q", tt.input)
		})
	}
}
