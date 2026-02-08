package agent

import (
	"context"
	"strings"

	"github.com/tinkerloft/fleetlift/internal/agent/protocol"
)

// runVerifiers executes all verifiers defined in the manifest.
// Returns a map of repo name → []VerifierResult.
func (p *Pipeline) runVerifiers(ctx context.Context, manifest *protocol.TaskManifest) map[string][]protocol.VerifierResult {
	if len(manifest.Verifiers) == 0 {
		return nil
	}

	results := make(map[string][]protocol.VerifierResult)
	repos := manifest.EffectiveRepos()

	for _, repo := range repos {
		repoPath := manifest.RepoPath(repo.Name)
		var repoResults []protocol.VerifierResult

		for _, verifier := range manifest.Verifiers {
			p.logger.Info("Running verifier", "verifier", verifier.Name, "repo", repo.Name)

			result := p.runVerifier(ctx, verifier, repoPath)
			repoResults = append(repoResults, result)

			if !result.Success {
				p.logger.Warn("Verifier failed", "verifier", verifier.Name, "repo", repo.Name, "exitCode", result.ExitCode)
			}
		}

		results[repo.Name] = repoResults
	}

	return results
}

func (p *Pipeline) runVerifier(ctx context.Context, verifier protocol.ManifestVerifier, workDir string) protocol.VerifierResult {
	if len(verifier.Command) == 0 {
		return protocol.VerifierResult{
			Name:     verifier.Name,
			Success:  false,
			ExitCode: -1,
			Output:   "empty command",
		}
	}

	result, err := p.exec.Run(ctx, CommandOpts{
		Name: verifier.Command[0],
		Args: verifier.Command[1:],
		Dir:  workDir,
	})

	exitCode := 0
	outputStr := ""
	if result != nil {
		exitCode = result.ExitCode
		outputStr = result.Stdout + result.Stderr
	}
	if err != nil && exitCode == 0 {
		exitCode = -1
	}

	// Truncate long output
	if len(outputStr) > MaxOutputTruncation {
		outputStr = outputStr[:MaxOutputTruncation] + "\n... [truncated]"
	}

	return protocol.VerifierResult{
		Name:     verifier.Name,
		Success:  exitCode == 0,
		ExitCode: exitCode,
		Output:   strings.TrimSpace(outputStr),
	}
}
