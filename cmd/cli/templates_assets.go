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
