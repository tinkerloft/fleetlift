# CLI Reference

Complete command-line reference for the Claude Code Orchestrator.

## Starting Workflows

### From YAML File

```bash
./bin/cli run -f task.yaml
```

### From Flags (Transform Mode)

```bash
./bin/cli run \
  --repo https://github.com/org/repo.git \
  --prompt "Fix the bug in auth.go" \
  --no-approval
```

### Report Mode (Discovery)

```bash
./bin/cli run -f examples/task-report.yaml
```

## Monitoring Workflows

### List Workflows

```bash
# List all workflows
./bin/cli list

# Filter by status
./bin/cli list --status Running
./bin/cli list --status Completed
./bin/cli list --status Failed
```

### Check Status (Running Workflows)

```bash
# Only works while workflow is running
./bin/cli status --workflow-id transform-<task-id>
```

### Get Result (Completed Workflows)

```bash
# Works for completed workflows
./bin/cli result --workflow-id transform-<task-id>
```

## Report Commands

### View Reports (Report Mode Only)

```bash
# Table format (body truncated for readability)
./bin/cli reports transform-<task-id>

# Full JSON output
./bin/cli reports transform-<task-id> -o json

# Save to file
./bin/cli reports transform-<task-id> -o json > report.json

# Only frontmatter (structured data)
./bin/cli reports transform-<task-id> --frontmatter-only -o json

# Filter to specific target (forEach mode)
./bin/cli reports transform-<task-id> --target users-api
```

## Workflow Control

### Approve Changes

```bash
# Approve when require_approval: true
./bin/cli approve --workflow-id transform-<task-id>
```

### Reject Changes

```bash
# Reject and stop workflow
./bin/cli reject --workflow-id transform-<task-id>
```

### Cancel Workflow

```bash
# Cancel running workflow
./bin/cli cancel --workflow-id transform-<task-id>
```

## Important Notes

### Workflow ID Prefix

Workflow IDs are automatically prefixed with `transform-`:

```yaml
# In task file
id: smoke-test

# Actual workflow ID
transform-smoke-test
```

Use `transform-<task-id>` in all CLI commands.

### Status vs Result

- **`status`**: Only works for **running** workflows
- **`result`**: Only works for **completed** workflows

```bash
# While running
./bin/cli status --workflow-id transform-my-task

# After completion
./bin/cli result --workflow-id transform-my-task
```

### Temporal UI

View detailed workflow execution:
```bash
open http://localhost:8233
```

Navigate to: `Namespaces > default > Workflows > transform-<task-id>`

## Examples

### Complete Workflow Lifecycle

```bash
# 1. Start workflow
./bin/cli run -f examples/task-agentic.yaml

# 2. Monitor progress
./bin/cli list --status Running

# 3. Check status while running
./bin/cli status --workflow-id transform-example-agentic

# 4. View in UI
open http://localhost:8233

# 5. Approve (if require_approval: true)
./bin/cli approve --workflow-id transform-example-agentic

# 6. Get final result
./bin/cli result --workflow-id transform-example-agentic
```

### Report Mode Workflow

```bash
# 1. Start discovery
./bin/cli run -f examples/task-report.yaml

# 2. Wait for completion (or check status)
./bin/cli status --workflow-id transform-example-report

# 3. View reports
./bin/cli reports transform-example-report

# 4. Export to JSON
./bin/cli reports transform-example-report -o json > results.json

# 5. Process structured data
cat results.json | jq '.[] | .frontmatter'
```

### forEach Workflow

```bash
# 1. Start forEach analysis
./bin/cli run -f examples/task-foreach.yaml

# 2. Get all results
./bin/cli reports transform-example-foreach -o json

# 3. Filter to specific target
./bin/cli reports transform-example-foreach --target users-api -o json
```

## Global Flags

```
--temporal-address string   Temporal server address (default "localhost:7233")
--namespace string          Temporal namespace (default "default")
--help                      Show help
--version                   Show version
```

## Environment Variables

```bash
# Required for agentic mode
export ANTHROPIC_API_KEY=sk-ant-...

# Optional, for creating PRs
export GITHUB_TOKEN=ghp_...

# Temporal server (if not localhost)
export TEMPORAL_ADDRESS=temporal.example.com:7233

# Temporal namespace (if not default)
export TEMPORAL_NAMESPACE=production
```

## Exit Codes

- `0` - Success
- `1` - General error
- `2` - Invalid arguments
- `3` - Workflow not found
- `4` - Workflow failed
- `5` - Approval timeout/rejected

## Tips

### Using jq with Reports

```bash
# Extract all security scores
./bin/cli reports transform-security-audit -o json | jq '.[] | .frontmatter.score'

# Find high-severity issues
./bin/cli reports transform-audit -o json | \
  jq '.[] | .frontmatter.issues[] | select(.severity == "high")'

# Group by repository
./bin/cli reports transform-audit -o json | jq 'group_by(.repository)'
```

### Debugging Workflows

```bash
# Get detailed execution history
open "http://localhost:8233/namespaces/default/workflows/transform-<task-id>"

# Check worker logs
make run-worker  # Worker outputs detailed logs

# View activity inputs/outputs in Temporal UI
# Navigate to: Workflow > History > Activity details
```

### Batch Operations

```bash
# List all failed workflows
./bin/cli list --status Failed

# Cancel all running workflows (careful!)
./bin/cli list --status Running | \
  awk '{print $1}' | \
  xargs -I {} ./bin/cli cancel --workflow-id {}
```

## See Also

- [Task File Reference](TASK_FILE_REFERENCE.md) - YAML specification
- [Examples](examples/) - Example workflows
- [Troubleshooting](TROUBLESHOOTING.md) - Common issues
