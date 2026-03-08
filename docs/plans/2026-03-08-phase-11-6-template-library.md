# Phase 11.6 Template Library Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a built-in template library embedded in the CLI binary + user-local templates at `~/.fleetlift/templates/`, accessible via `fleetlift create --template <name>` and `fleetlift templates list`.

**Architecture:** Four built-in YAML template files are embedded in the binary via `//go:embed`. A `Template` struct wraps name + description + content. A new `templates.go` file holds the registry + `fleetlift templates list` command. The `--template` flag on `create` loads a template, optionally substitutes `--repo` values, then delegates to the existing `confirmAndSave` flow. User templates at `~/.fleetlift/templates/*.yaml` are merged with built-ins at list/load time.

**Tech Stack:** Go, `embed`, cobra, testify, existing `model.Task` + `validateTaskYAML`.

---

### Task 1: Built-in template YAML files

**Files:**
- Create: `cmd/cli/templates/dependency-upgrade.yaml`
- Create: `cmd/cli/templates/api-migration.yaml`
- Create: `cmd/cli/templates/security-audit.yaml`
- Create: `cmd/cli/templates/framework-upgrade.yaml`

**Step 1: Create dependency-upgrade.yaml**

```yaml
version: 1
id: dependency-upgrade
title: "Dependency upgrade"
description: "Upgrade outdated dependencies to their latest compatible versions"
mode: transform

repositories:
  - url: https://github.com/your-org/your-repo.git
    branch: main

execution:
  agentic:
    prompt: |
      Upgrade all outdated dependencies to their latest compatible versions.

      Requirements:
      - Check for outdated direct dependencies
      - Upgrade to latest compatible (non-breaking) versions
      - Run the build and test suite to verify compatibility
      - Update lock files if present
      - Do not change any business logic
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 30m
require_approval: true

pull_request:
  branch_prefix: "auto/dependency-upgrade"
  title: "Upgrade dependencies to latest compatible versions"
  labels: ["automated", "dependencies"]
```

**Step 2: Create api-migration.yaml**

```yaml
version: 1
id: api-migration
title: "API migration"
description: "Migrate from a deprecated API version to a new one"
mode: transform

repositories:
  - url: https://github.com/your-org/your-repo.git
    branch: main

execution:
  agentic:
    prompt: |
      Migrate all usages of the deprecated API to the new API version.

      Requirements:
      - Identify all call sites using the old API
      - Replace with equivalent new API calls
      - Preserve behaviour and semantics
      - Update imports as needed
      - Do not change unrelated code
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 30m
require_approval: true

pull_request:
  branch_prefix: "auto/api-migration"
  title: "Migrate to updated API"
  labels: ["automated", "migration"]
```

**Step 3: Create security-audit.yaml**

```yaml
version: 1
id: security-audit
title: "Security audit"
description: "Audit repository for common security vulnerabilities and issues"
mode: report

repositories:
  - url: https://github.com/your-org/your-repo.git
    branch: main

execution:
  agentic:
    prompt: |
      Perform a security audit of this repository.

      Write your findings to /workspace/REPORT.md with YAML frontmatter:

      ---
      risk: low|medium|high|critical
      vulnerability_count: <count>
      categories: [list of issue categories]
      ---

      # Security Audit

      For each finding, describe: location, severity, issue, recommendation.
    output:
      schema:
        type: object
        required: [risk]
        properties:
          risk:
            type: string
            enum: [low, medium, high, critical]
          vulnerability_count:
            type: integer
          categories:
            type: array
            items:
              type: string

timeout: 20m
```

**Step 4: Create framework-upgrade.yaml**

```yaml
version: 1
id: framework-upgrade
title: "Framework upgrade"
description: "Upgrade a framework or runtime to a new major version"
mode: transform

repositories:
  - url: https://github.com/your-org/your-repo.git
    branch: main

execution:
  agentic:
    prompt: |
      Upgrade the framework to the new major version.

      Requirements:
      - Update the framework version in dependency manifests
      - Apply any required breaking-change migrations documented in the upgrade guide
      - Fix compilation errors caused by removed or changed APIs
      - Ensure tests pass after the upgrade
      - Do not change unrelated code
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 45m
require_approval: true

pull_request:
  branch_prefix: "auto/framework-upgrade"
  title: "Upgrade framework to new major version"
  labels: ["automated", "upgrade"]
```

**Step 5: Verify files exist**

```bash
ls cmd/cli/templates/
# dependency-upgrade.yaml  api-migration.yaml  security-audit.yaml  framework-upgrade.yaml
```

**Step 6: Commit**

```bash
git add cmd/cli/templates/
git commit -m "feat(templates): add 4 built-in template YAML stubs"
```

---

### Task 2: Embed templates + Template struct

**Files:**
- Create: `cmd/cli/templates_assets.go`

**Step 1: Write failing test** in `cmd/cli/templates_test.go`

```go
package main

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestBuiltinTemplates_Count(t *testing.T) {
    assert.Len(t, builtinTemplates, 4)
}

func TestBuiltinTemplates_AllHaveContent(t *testing.T) {
    for _, tmpl := range builtinTemplates {
        assert.NotEmpty(t, tmpl.Name, "template missing name")
        assert.NotEmpty(t, tmpl.Description, "template %q missing description", tmpl.Name)
        assert.NotEmpty(t, tmpl.Content, "template %q missing content", tmpl.Name)
    }
}

func TestBuiltinTemplates_ContentIsValidYAML(t *testing.T) {
    for _, tmpl := range builtinTemplates {
        _, err := validateTaskYAML(tmpl.Content)
        // Templates use placeholder repos so require_approval / repos may be set;
        // validateTaskYAML only checks title + execution + ≥1 repo.
        require.NoError(t, err, "template %q has invalid YAML", tmpl.Name)
    }
}

func TestFindTemplate_BuiltinFound(t *testing.T) {
    tmpl, err := findTemplate("security-audit")
    require.NoError(t, err)
    assert.Equal(t, "security-audit", tmpl.Name)
}

func TestFindTemplate_NotFound(t *testing.T) {
    _, err := findTemplate("nonexistent-template")
    assert.ErrorContains(t, err, "nonexistent-template")
}
```

**Step 2: Run test — expect compile failure** (types not defined yet)

```bash
cd cmd/cli && go test ./... 2>&1 | head -20
```

**Step 3: Create `cmd/cli/templates_assets.go`**

```go
package main

import (
    _ "embed"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "gopkg.in/yaml.v3"
)

//go:embed templates/dependency-upgrade.yaml
var tmplDependencyUpgrade string

//go:embed templates/api-migration.yaml
var tmplAPIMigration string

//go:embed templates/security-audit.yaml
var tmplSecurityAudit string

//go:embed templates/framework-upgrade.yaml
var tmplFrameworkUpgrade string

// Template is a named task YAML template.
type Template struct {
    Name        string
    Description string
    Content     string
}

// builtinTemplates is the registry of embedded templates.
var builtinTemplates = []Template{
    {Name: "dependency-upgrade", Description: "Upgrade outdated dependencies to latest compatible versions", Content: tmplDependencyUpgrade},
    {Name: "api-migration", Description: "Migrate from a deprecated API version to a new one", Content: tmplAPIMigration},
    {Name: "security-audit", Description: "Audit repository for common security vulnerabilities and issues", Content: tmplSecurityAudit},
    {Name: "framework-upgrade", Description: "Upgrade a framework or runtime to a new major version", Content: tmplFrameworkUpgrade},
}

// findTemplate returns the template with the given name, checking built-ins
// then user templates at ~/.fleetlift/templates/<name>.yaml.
func findTemplate(name string) (Template, error) {
    for _, t := range builtinTemplates {
        if t.Name == name {
            return t, nil
        }
    }

    // Check user templates
    tmpl, err := loadUserTemplate(name)
    if err == nil {
        return tmpl, nil
    }

    return Template{}, fmt.Errorf("template %q not found (use 'fleetlift templates list' to see available templates)", name)
}

// loadUserTemplate loads a template from ~/.fleetlift/templates/<name>.yaml.
func loadUserTemplate(name string) (Template, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return Template{}, err
    }

    path := filepath.Join(home, ".fleetlift", "templates", name+".yaml")
    data, err := os.ReadFile(path)
    if err != nil {
        return Template{}, err
    }

    content := string(data)
    desc := extractTemplateDescription(content)
    return Template{Name: name, Description: desc, Content: content}, nil
}

// listUserTemplates returns all templates from ~/.fleetlift/templates/.
func listUserTemplates() ([]Template, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return nil, err
    }

    dir := filepath.Join(home, ".fleetlift", "templates")
    entries, err := os.ReadDir(dir)
    if os.IsNotExist(err) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }

    var templates []Template
    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
            continue
        }
        name := strings.TrimSuffix(e.Name(), ".yaml")
        tmpl, err := loadUserTemplate(name)
        if err != nil {
            continue
        }
        templates = append(templates, tmpl)
    }
    return templates, nil
}

// extractTemplateDescription reads the `description` field from YAML content.
func extractTemplateDescription(content string) string {
    var raw struct {
        Description string `yaml:"description"`
    }
    if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
        return ""
    }
    return raw.Description
}
```

**Step 4: Run tests — expect PASS**

```bash
cd cmd/cli && go test -run "TestBuiltinTemplates|TestFindTemplate" -v
```

Expected:
```
--- PASS: TestBuiltinTemplates_Count
--- PASS: TestBuiltinTemplates_AllHaveContent
--- PASS: TestBuiltinTemplates_ContentIsValidYAML
--- PASS: TestFindTemplate_BuiltinFound
--- PASS: TestFindTemplate_NotFound
```

**Step 5: Commit**

```bash
git add cmd/cli/templates_assets.go cmd/cli/templates_test.go
git commit -m "feat(templates): embed built-in templates + Template registry"
```

---

### Task 3: `fleetlift templates list` command

**Files:**
- Create: `cmd/cli/templates.go`
- Modify: `cmd/cli/main.go`

**Step 1: Write failing test** (add to `templates_test.go`)

```go
func TestTemplatesListOutput_ContainsBuiltins(t *testing.T) {
    // allTemplates returns built-ins (user templates may not exist in test env).
    all := allTemplates()
    names := make([]string, len(all))
    for i, t := range all {
        names[i] = t.Name
    }
    assert.Contains(t, names, "dependency-upgrade")
    assert.Contains(t, names, "api-migration")
    assert.Contains(t, names, "security-audit")
    assert.Contains(t, names, "framework-upgrade")
}
```

**Step 2: Run test — expect compile failure** (`allTemplates` not defined)

```bash
cd cmd/cli && go test -run TestTemplatesListOutput -v 2>&1 | head -10
```

**Step 3: Create `cmd/cli/templates.go`**

```go
package main

import (
    "fmt"

    "github.com/spf13/cobra"
)

var templatesCmd = &cobra.Command{
    Use:   "templates",
    Short: "Manage task templates",
    Long:  "List and inspect built-in and user-defined task templates",
}

var templatesListCmd = &cobra.Command{
    Use:   "list",
    Short: "List available templates",
    RunE:  runTemplatesList,
}

func init() {
    templatesCmd.AddCommand(templatesListCmd)
}

func runTemplatesList(_ *cobra.Command, _ []string) error {
    all := allTemplates()
    if len(all) == 0 {
        fmt.Println("No templates available.")
        return nil
    }

    fmt.Printf("%-25s %s\n", "NAME", "DESCRIPTION")
    fmt.Printf("%-25s %s\n", "----", "-----------")
    for _, t := range all {
        fmt.Printf("%-25s %s\n", t.Name, t.Description)
    }
    fmt.Printf("\nUse: fleetlift create --template <name> [--repo <url>] [--output file.yaml]\n")
    return nil
}

// allTemplates returns built-in templates followed by user-local templates.
func allTemplates() []Template {
    all := make([]Template, len(builtinTemplates))
    copy(all, builtinTemplates)

    user, _ := listUserTemplates()
    all = append(all, user...)
    return all
}
```

**Step 4: Wire `templatesCmd` into `main.go`**

In `cmd/cli/main.go`, in the `init()` block, add after `rootCmd.AddCommand(createCmd)`:

```go
rootCmd.AddCommand(templatesCmd)
```

**Step 5: Run tests — expect PASS**

```bash
cd cmd/cli && go test -run TestTemplatesListOutput -v
```

**Step 6: Manual smoke test**

```bash
go run ./cmd/cli templates list
```

Expected output:
```
NAME                      DESCRIPTION
----                      -----------
dependency-upgrade        Upgrade outdated dependencies to latest compatible versions
api-migration             Migrate from a deprecated API version to a new one
security-audit            Audit repository for common security vulnerabilities and issues
framework-upgrade         Upgrade a framework or runtime to a new major version

Use: fleetlift create --template <name> [--repo <url>] [--output file.yaml]
```

**Step 7: Commit**

```bash
git add cmd/cli/templates.go cmd/cli/main.go cmd/cli/templates_test.go
git commit -m "feat(templates): add 'fleetlift templates list' command"
```

---

### Task 4: `--template` flag on `fleetlift create`

**Files:**
- Modify: `cmd/cli/create.go`
- Modify: `cmd/cli/create_test.go`

**Step 1: Write failing test** (add to `create_test.go`)

```go
func TestApplyRepoOverrides_ReplacesRepositories(t *testing.T) {
    yamlStr := `version: 1
title: "Test"
repositories:
  - url: https://github.com/your-org/your-repo.git
execution:
  agentic:
    prompt: "do it"
`
    result, err := applyRepoOverrides(yamlStr, []string{"https://github.com/acme/svc.git"})
    require.NoError(t, err)
    assert.Contains(t, result, "acme/svc.git")
    assert.NotContains(t, result, "your-org")
}

func TestApplyRepoOverrides_NoRepos_ReturnsUnchanged(t *testing.T) {
    yamlStr := "version: 1\ntitle: T\nrepositories:\n  - url: https://github.com/x/y.git\nexecution:\n  agentic:\n    prompt: p\n"
    result, err := applyRepoOverrides(yamlStr, nil)
    require.NoError(t, err)
    assert.Equal(t, yamlStr, result)
}
```

**Step 2: Run test — expect FAIL** (`applyRepoOverrides` not defined)

```bash
cd cmd/cli && go test -run TestApplyRepoOverrides -v 2>&1 | head -10
```

**Step 3: Add `applyRepoOverrides` + `runCreateFromTemplate` to `create.go`**

Add before `runCreate`:

```go
// applyRepoOverrides replaces the repositories list in a task YAML string
// with the provided repo URLs. If repos is empty, returns content unchanged.
func applyRepoOverrides(content string, repos []string) (string, error) {
    if len(repos) == 0 {
        return content, nil
    }

    var task model.Task
    if err := yaml.Unmarshal([]byte(content), &task); err != nil {
        return "", fmt.Errorf("parsing template YAML: %w", err)
    }

    task.Repositories = nil
    for _, u := range repos {
        task.Repositories = append(task.Repositories, model.NewRepository(u, "main", ""))
    }

    out, err := yaml.Marshal(&task)
    if err != nil {
        return "", fmt.Errorf("marshalling YAML: %w", err)
    }
    return string(out), nil
}

func runCreateFromTemplate(cmd *cobra.Command, templateName, outputPath string, repos []string, dryRun, runAfter bool) error {
    tmpl, err := findTemplate(templateName)
    if err != nil {
        return err
    }

    yamlStr, err := applyRepoOverrides(tmpl.Content, repos)
    if err != nil {
        return fmt.Errorf("applying repo overrides: %w", err)
    }

    if _, valErr := validateTaskYAML(yamlStr); valErr != nil {
        fmt.Fprintf(os.Stderr, "Warning: template YAML may have issues: %v\n", valErr)
    }

    fmt.Printf("Template: %s\n", tmpl.Name)
    fmt.Println("---")
    fmt.Print(yamlStr)
    fmt.Println("---")

    if dryRun {
        return nil
    }

    if runAfter {
        if outputPath == "" {
            return fmt.Errorf("--run requires --output")
        }
        if err := os.WriteFile(outputPath, []byte(yamlStr), 0o644); err != nil {
            return fmt.Errorf("writing file: %w", err)
        }
        fmt.Printf("Saved to %s\n", outputPath)
        return startRunFromFile(cmd.Context(), outputPath)
    }

    return confirmAndSave(yamlStr, outputPath)
}
```

**Step 4: Add `--template` flag in `init()` and wire in `runCreate`**

In `cmd/cli/create.go`, add to `init()`:
```go
createCmd.Flags().String("template", "", "Start from a named template (use 'fleetlift templates list' to see options)")
```

In `runCreate`, after the `interactive` block and before the `description == ""` check:
```go
templateName, _ := cmd.Flags().GetString("template")
if templateName != "" {
    return runCreateFromTemplate(cmd, templateName, outputPath, repos, dryRun, runAfter)
}
```

**Step 5: Run tests — expect PASS**

```bash
cd cmd/cli && go test -run "TestApplyRepoOverrides" -v
```

**Step 6: Full test suite**

```bash
cd cmd/cli && go test ./...
```

Expected: all PASS

**Step 7: Smoke test**

```bash
go run ./cmd/cli create --template security-audit --dry-run
```

Expected: prints the security-audit template YAML with `Template: security-audit` header.

```bash
go run ./cmd/cli create --template api-migration --repo https://github.com/acme/svc.git --dry-run
```

Expected: `acme/svc.git` appears in repositories, placeholder replaced.

**Step 8: Commit**

```bash
git add cmd/cli/create.go cmd/cli/create_test.go
git commit -m "feat(templates): add --template flag to 'fleetlift create'"
```

---

### Task 5: User templates + final verification

**Files:**
- Modify: `cmd/cli/templates_test.go`

**Step 1: Write test for `listUserTemplates` (no panic on missing dir)**

```go
func TestListUserTemplates_EmptyDir(t *testing.T) {
    // Point user templates at a temp dir that doesn't exist — should return nil, nil.
    // We can't easily redirect os.UserHomeDir, so test via the exported helper with a
    // known-empty temp dir approach by testing that the function doesn't error when
    // ~/.fleetlift/templates doesn't exist.
    templates, err := listUserTemplates()
    // Either nil error with empty slice (dir missing) or populated slice — never an error
    // for missing dir.
    assert.NoError(t, err)
    _ = templates // may be nil or populated depending on user's machine
}
```

**Step 2: Run test — expect PASS**

```bash
cd cmd/cli && go test -run TestListUserTemplates -v
```

**Step 3: Run full lint + tests**

```bash
make lint && go test ./...
```

Expected: no errors.

**Step 4: Build verification**

```bash
go build ./...
```

**Step 5: Commit**

```bash
git add cmd/cli/templates_test.go
git commit -m "test(templates): add user-templates smoke test + Phase 11.6 complete"
```

---

## Summary

After completing all tasks:

```bash
# List all templates
fleetlift templates list

# Use a built-in template (dry run)
fleetlift create --template security-audit --dry-run

# Use template + inject repos + save
fleetlift create --template dependency-upgrade \
  --repo https://github.com/acme/api.git \
  --output dep-upgrade.yaml

# User templates: drop YAML files in ~/.fleetlift/templates/ and they appear in list
```

## Unanswered Questions

- Should `--template` combined with `--describe` use Claude to refine the template (i.e., pass template YAML + description to Claude)? Currently `--template` takes precedence and skips Claude entirely.
- Should `applyRepoOverrides` preserve all non-`repositories` fields exactly (current: round-trips through `model.Task` marshal which may reorder keys)?
