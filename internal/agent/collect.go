package agent

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// collectResults gathers diffs, modified files, and reports for each repo.
func (p *Pipeline) collectResults(ctx context.Context, manifest *protocol.TaskManifest, verifierResults map[string][]protocol.VerifierResult) []protocol.RepoResult {
	repos := manifest.EffectiveRepos()
	var results []protocol.RepoResult

	for _, repo := range repos {
		repoPath := manifest.RepoPath(repo.Name)
		result := protocol.RepoResult{
			Name:   repo.Name,
			Status: "success",
		}

		// Collect modified files via git status
		filesModified := p.getModifiedFiles(ctx, repoPath)
		result.FilesModified = filesModified

		// Collect diffs
		if len(filesModified) > 0 {
			result.Diffs = p.getDiffs(ctx, repoPath)
		}

		// Attach verifier results
		if vr, ok := verifierResults[repo.Name]; ok {
			result.VerifierResults = vr
			for _, v := range vr {
				if !v.Success {
					result.Status = "failed"
					errMsg := "verifier " + v.Name + " failed"
					result.Error = &errMsg
					break
				}
			}
		}

		// Collect reports (report mode)
		if manifest.Mode == "report" {
			if len(manifest.ForEach) > 0 {
				// forEach mode: collect per-target reports
				for _, target := range manifest.ForEach {
					reportPath := filepath.Join(repoPath, "REPORT-"+target.Name+".md")
					report := p.readReport(reportPath)
					result.ForEachResults = append(result.ForEachResults, protocol.ForEachResult{
						Target: target,
						Report: report,
					})
				}
			} else {
				// Single report
				reportPath := filepath.Join(repoPath, "REPORT.md")
				result.Report = p.readReport(reportPath)
			}
		}

		results = append(results, result)
	}

	return results
}

func (p *Pipeline) getModifiedFiles(ctx context.Context, repoPath string) []string {
	// Use git status --porcelain which reports both tracked changes and untracked files.
	result, err := p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: []string{"status", "--porcelain"},
		Dir:  repoPath,
	})
	if err != nil || result == nil {
		p.logger.Warn("git status failed", "path", repoPath, "error", err)
		return nil
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line == "" {
			continue
		}
		// Porcelain format: XY filename (or XY old -> new for renames)
		if len(line) > 3 {
			entry := strings.TrimSpace(line[3:])
			// Handle renames: "old -> new"
			if idx := strings.Index(entry, " -> "); idx >= 0 {
				entry = entry[idx+4:]
			}
			files = append(files, entry)
		}
	}
	return files
}

func (p *Pipeline) getDiffs(ctx context.Context, repoPath string) []protocol.DiffEntry {
	// Get per-file diffs
	fullDiffResult, err := p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: []string{"diff", "HEAD"},
		Dir:  repoPath,
	})
	fullDiff := ""
	if err == nil && fullDiffResult != nil {
		fullDiff = fullDiffResult.Stdout
	} else {
		// Try without HEAD (uncommitted new files)
		r, _ := p.exec.Run(ctx, CommandOpts{
			Name: "git",
			Args: []string{"diff"},
			Dir:  repoPath,
		})
		if r != nil {
			fullDiff = r.Stdout
		}
	}

	// Also get cached diffs
	cachedDiff := ""
	r, _ := p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: []string{"diff", "--cached"},
		Dir:  repoPath,
	})
	if r != nil {
		cachedDiff = r.Stdout
	}

	// Get numstat for additions/deletions
	numstat := ""
	r, _ = p.exec.Run(ctx, CommandOpts{
		Name: "git",
		Args: []string{"diff", "--numstat", "HEAD"},
		Dir:  repoPath,
	})
	if r != nil {
		numstat = r.Stdout
	}

	// Parse numstat into a map
	statMap := parseNumstat(numstat)

	// Parse full diff into per-file entries
	entries := parseDiffOutput(fullDiff, cachedDiff, statMap)

	return entries
}

func parseNumstat(numstat string) map[string][2]int {
	result := make(map[string][2]int)
	for _, line := range strings.Split(strings.TrimSpace(numstat), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			adds, _ := strconv.Atoi(parts[0])
			dels, _ := strconv.Atoi(parts[1])
			result[parts[2]] = [2]int{adds, dels}
		}
	}
	return result
}

func parseDiffOutput(fullDiff, cachedDiff string, statMap map[string][2]int) []protocol.DiffEntry {
	combinedDiff := fullDiff
	if cachedDiff != "" {
		combinedDiff += "\n" + cachedDiff
	}

	if strings.TrimSpace(combinedDiff) == "" {
		return nil
	}

	var entries []protocol.DiffEntry
	// Split by "diff --git" markers
	parts := strings.Split(combinedDiff, "diff --git ")
	for _, part := range parts[1:] { // skip empty first element
		lines := strings.SplitN(part, "\n", 2)
		if len(lines) == 0 {
			continue
		}

		// Parse file path from "a/path b/path"
		header := lines[0]
		fields := strings.Fields(header)
		if len(fields) < 2 {
			continue
		}
		filePath := strings.TrimPrefix(fields[1], "b/")

		// Determine status
		status := "modified"
		if strings.Contains(part, "new file mode") {
			status = "added"
		} else if strings.Contains(part, "deleted file mode") {
			status = "deleted"
		}

		// Get additions/deletions from numstat
		adds, dels := 0, 0
		if stat, ok := statMap[filePath]; ok {
			adds = stat[0]
			dels = stat[1]
		}

		// Truncate long diffs
		diffContent := "diff --git " + part
		diffLines := strings.Split(diffContent, "\n")
		if len(diffLines) > protocol.MaxDiffLinesPerFile {
			diffContent = strings.Join(diffLines[:protocol.MaxDiffLinesPerFile], "\n") + "\n... [truncated]"
		}

		entries = append(entries, protocol.DiffEntry{
			Path:      filePath,
			Status:    status,
			Additions: adds,
			Deletions: dels,
			Diff:      diffContent,
		})
	}

	return entries
}

func (p *Pipeline) readReport(path string) *protocol.ReportResult {
	data, err := p.fs.ReadFile(path)
	if err != nil {
		return nil
	}

	raw := string(data)
	report := &protocol.ReportResult{Raw: raw}

	// Parse frontmatter (between --- delimiters)
	if strings.HasPrefix(raw, "---\n") {
		parts := strings.SplitN(raw[4:], "\n---\n", 2)
		if len(parts) == 2 {
			report.Body = strings.TrimSpace(parts[1])
			// H5 fix: use proper YAML parser instead of hand-rolled one
			report.Frontmatter = parseYAMLFrontmatter(parts[0])
		}
	}

	return report
}

// parseYAMLFrontmatter parses YAML frontmatter into a map using gopkg.in/yaml.v3.
func parseYAMLFrontmatter(yamlStr string) map[string]any {
	result := make(map[string]any)
	if err := yaml.Unmarshal([]byte(yamlStr), &result); err != nil {
		return nil
	}
	return result
}
