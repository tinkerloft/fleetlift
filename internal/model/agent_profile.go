package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Marketplace struct {
	ID         string    `db:"id"         json:"id"`
	Name       string    `db:"name"       json:"name"`
	RepoURL    string    `db:"repo_url"   json:"repo_url"`
	Credential string    `db:"credential" json:"credential"`
	TeamID     *string   `db:"team_id"    json:"team_id,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

type AgentProfile struct {
	ID          string           `db:"id"          json:"id"`
	TeamID      *string          `db:"team_id"     json:"team_id,omitempty"`
	Name        string           `db:"name"        json:"name"`
	Description string           `db:"description" json:"description"`
	Body        AgentProfileBody `db:"body"        json:"body"`
	CreatedAt   time.Time        `db:"created_at"  json:"created_at"`
	UpdatedAt   time.Time        `db:"updated_at"  json:"updated_at"`
}

type AgentProfileBody struct {
	Plugins []PluginSource `json:"plugins,omitempty"`
	Skills  []SkillSource  `json:"skills,omitempty"`
	MCPs    []MCPConfig    `json:"mcps,omitempty"`
}

type PluginSource struct {
	Marketplace string `json:"marketplace,omitempty"`
	Plugin      string `json:"plugin,omitempty"`
	GitHubURL   string `json:"github_url,omitempty"`
}

func (p PluginSource) Validate() error {
	hasPlugin := p.Plugin != ""
	hasURL := p.GitHubURL != ""
	if hasPlugin && hasURL {
		return errors.New("plugin_source: only one of plugin or github_url may be set")
	}
	if !hasPlugin && !hasURL {
		return errors.New("plugin_source: one of plugin or github_url must be set")
	}
	if hasURL && !strings.HasPrefix(p.GitHubURL, "https://") {
		return fmt.Errorf("plugin_source: github_url must use https:// scheme, got %q", p.GitHubURL)
	}
	return nil
}

func (p PluginSource) DeduplicationKey() string {
	if p.GitHubURL != "" {
		return "url:" + p.GitHubURL
	}
	return "plugin:" + p.Plugin
}

type SkillSource struct {
	Marketplace string `json:"marketplace,omitempty"`
	Skill       string `json:"skill,omitempty"`
	GitHubURL   string `json:"github_url,omitempty"`
}

func (s SkillSource) Validate() error {
	hasSkill := s.Skill != ""
	hasURL := s.GitHubURL != ""
	if hasSkill && hasURL {
		return errors.New("skill_source: only one of skill or github_url may be set")
	}
	if !hasSkill && !hasURL {
		return errors.New("skill_source: one of skill or github_url must be set")
	}
	if hasURL && !strings.HasPrefix(s.GitHubURL, "https://") {
		return fmt.Errorf("skill_source: github_url must use https:// scheme, got %q", s.GitHubURL)
	}
	return nil
}

func (s SkillSource) DeduplicationKey() string {
	if s.GitHubURL != "" {
		return "url:" + s.GitHubURL
	}
	return "skill:" + s.Skill
}

type MCPConfig struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Transport   string   `json:"transport"`
	URL         string   `json:"url"`
	Headers     []Header `json:"headers,omitempty"`
	Credentials []string `json:"credentials,omitempty"`
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func MergeProfiles(baseline, workflowProfile *AgentProfileBody) AgentProfileBody {
	result := AgentProfileBody{}

	seen := map[string]int{}
	for _, p := range bodyPlugins(baseline) {
		k := p.DeduplicationKey()
		seen[k] = len(result.Plugins)
		result.Plugins = append(result.Plugins, p)
	}
	for _, p := range bodyPlugins(workflowProfile) {
		k := p.DeduplicationKey()
		if i, ok := seen[k]; ok {
			result.Plugins[i] = p
		} else {
			seen[k] = len(result.Plugins)
			result.Plugins = append(result.Plugins, p)
		}
	}

	seenS := map[string]int{}
	for _, s := range bodySkills(baseline) {
		k := s.DeduplicationKey()
		seenS[k] = len(result.Skills)
		result.Skills = append(result.Skills, s)
	}
	for _, s := range bodySkills(workflowProfile) {
		k := s.DeduplicationKey()
		if i, ok := seenS[k]; ok {
			result.Skills[i] = s
		} else {
			seenS[k] = len(result.Skills)
			result.Skills = append(result.Skills, s)
		}
	}

	seenM := map[string]int{}
	for _, m := range bodyMCPs(baseline) {
		seenM[m.Name] = len(result.MCPs)
		result.MCPs = append(result.MCPs, m)
	}
	for _, m := range bodyMCPs(workflowProfile) {
		if i, ok := seenM[m.Name]; ok {
			result.MCPs[i] = m
		} else {
			seenM[m.Name] = len(result.MCPs)
			result.MCPs = append(result.MCPs, m)
		}
	}

	return result
}

func bodyPlugins(b *AgentProfileBody) []PluginSource {
	if b == nil {
		return nil
	}
	return b.Plugins
}

func bodySkills(b *AgentProfileBody) []SkillSource {
	if b == nil {
		return nil
	}
	return b.Skills
}

func bodyMCPs(b *AgentProfileBody) []MCPConfig {
	if b == nil {
		return nil
	}
	return b.MCPs
}
