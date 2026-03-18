package activity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"

	"go.temporal.io/sdk/temporal"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/shellquote"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

// RunPreflight installs marketplace plugins and MCPs in the sandbox, then clones eval plugins.
func (a *Activities) RunPreflight(ctx context.Context, input workflow.RunPreflightInput) (workflow.RunPreflightOutput, error) {
	marketplaceURL := ""
	marketplaceToken := "" // resolved credential value for private marketplace auth
	// Look up marketplace URL and credential from DB if available.
	if a.DB != nil {
		var m struct {
			RepoURL    string  `db:"repo_url"`
			Credential *string `db:"credential"`
		}
		err := a.DB.GetContext(ctx, &m,
			`SELECT repo_url, credential FROM marketplaces
			  WHERE team_id IS NULL OR team_id = $1
			  ORDER BY team_id IS NULL ASC LIMIT 1`, input.TeamID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return workflow.RunPreflightOutput{}, fmt.Errorf("fetch marketplace config: %w", err)
		}
		if err == nil {
			marketplaceURL = m.RepoURL
			if m.Credential != nil && *m.Credential != "" && a.CredStore != nil {
				token, err := a.CredStore.Get(ctx, input.TeamID, *m.Credential)
				if err != nil {
					return workflow.RunPreflightOutput{}, fmt.Errorf("fetch marketplace credential: %w", err)
				}
				marketplaceToken = token
			}
		}
	}

	script := BuildPreflightScript(input.Profile, marketplaceURL, marketplaceToken)
	if script != "" {
		if _, stderr, err := a.Sandbox.Exec(ctx, input.SandboxID, script, "/"); err != nil {
			return workflow.RunPreflightOutput{}, fmt.Errorf("pre-flight script: %w\nstderr: %s", err, stderr)
		}
	}

	if len(input.EvalPluginURLs) == 0 {
		return workflow.RunPreflightOutput{}, nil
	}

	cloneResults, err := BuildEvalCloneCommands(input.EvalPluginURLs)
	if err != nil {
		return workflow.RunPreflightOutput{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("build eval clone commands: %s", err),
			"InvalidEvalPlugin", nil,
		)
	}

	var dirs []string
	for i, res := range cloneResults {
		if _, stderr, err := a.Sandbox.Exec(ctx, input.SandboxID, res.Command, "/"); err != nil {
			return workflow.RunPreflightOutput{}, fmt.Errorf("clone eval plugin %d: %w\nstderr: %s", i, err, stderr)
		}
		dirs = append(dirs, res.PluginDir)
	}

	return workflow.RunPreflightOutput{EvalPluginDirs: dirs}, nil
}

// BuildPreflightScript generates the shell script to install marketplace plugins and MCPs.
// marketplaceToken is the resolved credential value for private marketplace auth (empty = public).
func BuildPreflightScript(profile model.AgentProfileBody, marketplaceURL, marketplaceToken string) string {
	var b strings.Builder

	hasMarketplacePlugins := false
	for _, p := range profile.Plugins {
		if p.Plugin != "" {
			hasMarketplacePlugins = true
			break
		}
	}
	if hasMarketplacePlugins && marketplaceURL != "" {
		if marketplaceToken != "" {
			b.WriteString("git config --global credential.helper store\n")
			// Write the token directly into .git-credentials so it persists across commands.
			fmt.Fprintf(&b, "echo %s > ~/.git-credentials\n",
				shellquote.Quote("https://x-access-token:"+marketplaceToken+"@github.com"))
		}
		fmt.Fprintf(&b, "claude plugin marketplace add %s\n", shellquote.Quote(marketplaceURL))
	}

	for _, p := range profile.Plugins {
		if p.Plugin == "" {
			continue
		}
		pluginName := path.Base(p.Plugin)
		fmt.Fprintf(&b, "claude plugin uninstall %s 2>/dev/null || true\n", shellquote.Quote(pluginName))
		fmt.Fprintf(&b, "claude plugin install %s\n", shellquote.Quote(pluginName))
	}

	for _, mcp := range profile.MCPs {
		fmt.Fprintf(&b, "claude mcp remove %s 2>/dev/null || true\n", shellquote.Quote(mcp.Name))
		transport := mcp.Transport
		if transport == "" {
			transport = "sse"
		}
		fmt.Fprintf(&b, "claude mcp add --transport %s --scope user %s %s",
			shellquote.Quote(transport),
			shellquote.Quote(mcp.Name),
			shellquote.Quote(mcp.URL),
		)
		for _, h := range mcp.Headers {
			fmt.Fprintf(&b, " --header %s", shellquote.Quote(h.Name+": "+h.Value))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// EvalCloneResult holds the command to execute and the resulting plugin directory path.
type EvalCloneResult struct {
	Command   string
	PluginDir string // full path including subpath, e.g. /tmp/eval-plugin-0/plugins/foo
}

// BuildEvalCloneCommands generates git clone commands for eval plugin GitHub URLs.
func BuildEvalCloneCommands(urls []string) ([]EvalCloneResult, error) {
	var results []EvalCloneResult
	for i, rawURL := range urls {
		if !strings.HasPrefix(rawURL, "https://") {
			return nil, fmt.Errorf("eval_plugin url must use https:// scheme, got %q", rawURL)
		}
		repoURL, subPath, err := ParseGitHubTreeURL(rawURL)
		if err != nil {
			return nil, fmt.Errorf("parse eval plugin url %q: %w", rawURL, err)
		}
		dir := fmt.Sprintf("/tmp/eval-plugin-%d", i)
		cmd := fmt.Sprintf(
			"git clone --depth 1 --filter=blob:none --sparse %s %s && cd %s && git sparse-checkout set %s",
			shellquote.Quote(repoURL),
			shellquote.Quote(dir),
			shellquote.Quote(dir),
			shellquote.Quote(subPath),
		)
		results = append(results, EvalCloneResult{
			Command:   cmd,
			PluginDir: path.Join(dir, subPath),
		})
	}
	return results, nil
}

// ParseGitHubTreeURL parses a GitHub tree URL into a repo clone URL and subpath.
// Example: "https://github.com/org/repo/tree/main/plugins/foo" -> ("https://github.com/org/repo.git", "plugins/foo")
func ParseGitHubTreeURL(u string) (string, string, error) {
	trimmed := strings.TrimPrefix(u, "https://github.com/")
	parts := strings.SplitN(trimmed, "/tree/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected GitHub tree URL with /tree/ component, got %q", u)
	}
	repoPath := parts[0]
	branchAndSub := parts[1]
	subParts := strings.SplitN(branchAndSub, "/", 2)
	if len(subParts) < 2 {
		return "", "", fmt.Errorf("expected branch and subpath after /tree/ in %q", u)
	}
	return "https://github.com/" + repoPath + ".git", subParts[1], nil
}
