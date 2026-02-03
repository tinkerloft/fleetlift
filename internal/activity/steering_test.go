package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGitStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []fileStatus
	}{
		{
			name:  "modified file",
			input: " M src/main.go",
			expected: []fileStatus{
				{path: "src/main.go", status: "modified"},
			},
		},
		{
			name:  "staged modified file",
			input: "M  src/main.go",
			expected: []fileStatus{
				{path: "src/main.go", status: "modified"},
			},
		},
		{
			name:  "both staged and unstaged modified",
			input: "MM src/main.go",
			expected: []fileStatus{
				{path: "src/main.go", status: "modified"},
			},
		},
		{
			name:  "added file",
			input: "A  newfile.go",
			expected: []fileStatus{
				{path: "newfile.go", status: "added"},
			},
		},
		{
			name:  "untracked file",
			input: "?? untracked.txt",
			expected: []fileStatus{
				{path: "untracked.txt", status: "added"},
			},
		},
		{
			name:  "deleted file",
			input: "D  removed.go",
			expected: []fileStatus{
				{path: "removed.go", status: "deleted"},
			},
		},
		{
			name:  "renamed file",
			input: "R  old.go -> new.go",
			expected: []fileStatus{
				{path: "new.go", status: "modified"},
			},
		},
		{
			name: "multiple files",
			input: ` M file1.go
A  file2.go
?? file3.go
D  file4.go`,
			expected: []fileStatus{
				{path: "file1.go", status: "modified"},
				{path: "file2.go", status: "added"},
				{path: "file3.go", status: "added"},
				{path: "file4.go", status: "deleted"},
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			input:    "   \n  \n",
			expected: nil,
		},
		{
			name:  "file with spaces in name",
			input: " M my file.go",
			expected: []fileStatus{
				{path: "my file.go", status: "modified"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGitStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountAdditionsDeletions(t *testing.T) {
	tests := []struct {
		name              string
		diff              string
		expectedAdditions int
		expectedDeletions int
	}{
		{
			name: "simple additions and deletions",
			diff: `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 unchanged line
-deleted line
+added line
 another unchanged`,
			expectedAdditions: 1,
			expectedDeletions: 1,
		},
		{
			name: "only additions",
			diff: `+new line 1
+new line 2
+new line 3`,
			expectedAdditions: 3,
			expectedDeletions: 0,
		},
		{
			name: "only deletions",
			diff: `-old line 1
-old line 2`,
			expectedAdditions: 0,
			expectedDeletions: 2,
		},
		{
			name: "diff headers should not count",
			diff: `--- a/file.go
+++ b/file.go
@@ -1,1 +1,1 @@
-actual deletion
+actual addition`,
			expectedAdditions: 1,
			expectedDeletions: 1,
		},
		{
			name:              "empty diff",
			diff:              "",
			expectedAdditions: 0,
			expectedDeletions: 0,
		},
		{
			name: "lines starting with multiple + or -",
			diff: `++not an addition (double plus)
--not a deletion (double minus)
+actual addition
-actual deletion`,
			expectedAdditions: 1,
			expectedDeletions: 1,
		},
		{
			name: "context lines",
			diff: ` context line 1
 context line 2
+addition
-deletion
 context line 3`,
			expectedAdditions: 1,
			expectedDeletions: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			additions, deletions := countAdditionsDeletions(tt.diff)
			assert.Equal(t, tt.expectedAdditions, additions, "additions mismatch")
			assert.Equal(t, tt.expectedDeletions, deletions, "deletions mismatch")
		})
	}
}

func TestBuildSteeringPrompt(t *testing.T) {
	tests := []struct {
		name            string
		basePrompt      string
		steeringPrompt  string
		iteration       int
		previousOutput  string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:           "first iteration without previous output",
			basePrompt:     "Fix the bug in auth.go",
			steeringPrompt: "Also add error handling",
			iteration:      1,
			previousOutput: "",
			wantContains: []string{
				"## Steering Iteration 1",
				"### Original Task",
				"Fix the bug in auth.go",
				"### Feedback",
				"Also add error handling",
				"Please address the feedback",
			},
			wantNotContains: []string{
				"### Previous Changes Summary",
			},
		},
		{
			name:           "second iteration with previous output",
			basePrompt:     "Update the database schema",
			steeringPrompt: "Add index on user_id column",
			iteration:      2,
			previousOutput: "Added migration for new table",
			wantContains: []string{
				"## Steering Iteration 2",
				"### Original Task",
				"Update the database schema",
				"### Feedback",
				"Add index on user_id column",
				"### Previous Changes Summary",
				"Added migration for new table",
				"Build on your previous work",
			},
		},
		{
			name:           "handles empty strings",
			basePrompt:     "",
			steeringPrompt: "",
			iteration:      1,
			previousOutput: "",
			wantContains: []string{
				"## Steering Iteration 1",
				"### Original Task",
				"### Feedback",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildSteeringPrompt(tt.basePrompt, tt.steeringPrompt, tt.iteration, tt.previousOutput)

			for _, want := range tt.wantContains {
				assert.Contains(t, result, want, "should contain: %s", want)
			}

			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, result, notWant, "should not contain: %s", notWant)
			}
		})
	}
}
