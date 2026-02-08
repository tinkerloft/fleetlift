package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

func TestParseNumstat_Standard(t *testing.T) {
	input := "10\t5\tmain.go\n20\t0\tnew.go\n"
	result := parseNumstat(input)

	require.Len(t, result, 2)
	assert.Equal(t, [2]int{10, 5}, result["main.go"])
	assert.Equal(t, [2]int{20, 0}, result["new.go"])
}

func TestParseNumstat_BinaryFile(t *testing.T) {
	input := "-\t-\timage.png\n10\t5\tmain.go\n"
	result := parseNumstat(input)

	// Binary files have "-" which won't parse to int, so they get 0,0
	assert.Equal(t, [2]int{0, 0}, result["image.png"])
	assert.Equal(t, [2]int{10, 5}, result["main.go"])
}

func TestParseNumstat_Empty(t *testing.T) {
	result := parseNumstat("")
	assert.Empty(t, result)
}

func TestParseDiffOutput_AddModifyDelete(t *testing.T) {
	fullDiff := `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
diff --git a/old.go b/old.go
deleted file mode 100644
--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package old
`

	statMap := map[string][2]int{
		"main.go": {1, 0},
		"new.go":  {3, 0},
		"old.go":  {0, 3},
	}

	entries := parseDiffOutput(fullDiff, "", statMap)

	require.Len(t, entries, 3)

	assert.Equal(t, "main.go", entries[0].Path)
	assert.Equal(t, "modified", entries[0].Status)
	assert.Equal(t, 1, entries[0].Additions)

	assert.Equal(t, "new.go", entries[1].Path)
	assert.Equal(t, "added", entries[1].Status)

	assert.Equal(t, "old.go", entries[2].Path)
	assert.Equal(t, "deleted", entries[2].Status)
}

func TestParseDiffOutput_EmptyInput(t *testing.T) {
	result := parseDiffOutput("", "", nil)
	assert.Nil(t, result)
}

func TestParseDiffOutput_CachedDiff(t *testing.T) {
	cached := `diff --git a/staged.go b/staged.go
new file mode 100644
+++ b/staged.go
@@ -0,0 +1 @@
+package staged
`
	entries := parseDiffOutput("", cached, nil)
	require.Len(t, entries, 1)
	assert.Equal(t, "staged.go", entries[0].Path)
	assert.Equal(t, "added", entries[0].Status)
}

func TestParseYAMLFrontmatter_NestedStructures(t *testing.T) {
	yamlStr := `title: My Report
score: 8
tags:
  - security
  - auth
nested:
  key: value`

	result := parseYAMLFrontmatter(yamlStr)

	require.NotNil(t, result)
	assert.Equal(t, "My Report", result["title"])
	assert.Equal(t, 8, result["score"])
	// YAML v3 properly handles arrays and nested objects
	assert.IsType(t, []any{}, result["tags"])
	assert.IsType(t, map[string]any{}, result["nested"])
}

func TestParseYAMLFrontmatter_Invalid(t *testing.T) {
	// YAML v3 parses ":: invalid yaml [[" as a valid key-value pair (key ":" -> value "invalid yaml [[")
	// To test truly invalid YAML that causes an error, we need syntax that actually fails to parse
	result := parseYAMLFrontmatter("{ invalid: yaml: : :")
	assert.Nil(t, result)
}

func TestReadReport_WithFrontmatter(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	content := "---\ntitle: Report\nscore: 9\n---\n# My Report\n\nContent here."
	fs.files["/workspace/svc/REPORT.md"] = []byte(content)

	report := p.readReport("/workspace/svc/REPORT.md")

	require.NotNil(t, report)
	assert.Equal(t, content, report.Raw)
	assert.Equal(t, "# My Report\n\nContent here.", report.Body)
	assert.Equal(t, "Report", report.Frontmatter["title"])
	assert.Equal(t, 9, report.Frontmatter["score"])
}

func TestReadReport_NoFrontmatter(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	content := "# Just a report\nNo frontmatter."
	fs.files["/workspace/svc/REPORT.md"] = []byte(content)

	report := p.readReport("/workspace/svc/REPORT.md")

	require.NotNil(t, report)
	assert.Equal(t, content, report.Raw)
	assert.Nil(t, report.Frontmatter)
}

func TestReadReport_FileNotFound(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	p := testPipeline(fs, exec)

	report := p.readReport("/nonexistent/REPORT.md")
	assert.Nil(t, report)
}

func TestCollectResults_ReportMode_ForEach(t *testing.T) {
	fs := newMockFS()
	exec := newMockExecutor()
	exec.runFunc = func(ctx context.Context, opts CommandOpts) (*CommandResult, error) {
		return &CommandResult{Stdout: "", ExitCode: 0}, nil
	}
	p := testPipeline(fs, exec)

	// Write per-target report files
	fs.files["/workspace/svc/REPORT-users-api.md"] = []byte("---\nscore: 8\n---\nGood")
	fs.files["/workspace/svc/REPORT-orders-api.md"] = []byte("---\nscore: 5\n---\nNeeds work")

	manifest := &protocol.TaskManifest{
		Mode: "report",
		Repositories: []protocol.ManifestRepo{
			{Name: "svc"},
		},
		ForEach: []protocol.ForEachTarget{
			{Name: "users-api", Context: "GET /users"},
			{Name: "orders-api", Context: "GET /orders"},
		},
	}

	results := p.collectResults(testCtx(), manifest, nil)

	require.Len(t, results, 1)
	require.Len(t, results[0].ForEachResults, 2)
	assert.Equal(t, "users-api", results[0].ForEachResults[0].Target.Name)
	require.NotNil(t, results[0].ForEachResults[0].Report)
	assert.Equal(t, 8, results[0].ForEachResults[0].Report.Frontmatter["score"])
}

func testCtx() context.Context {
	return context.Background()
}
