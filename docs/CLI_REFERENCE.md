# CLI Reference

Complete command-line reference for the Claude Code Orchestrator.

## Workflow ID Memory

Fleetlift automatically remembers the last workflow you started. This makes it easy to run follow-up commands without specifying the workflow ID each time.

```bash
# Start a workflow
./bin/fleetlift run -f task.yaml
# Workflow started: transform-my-task-1738512345

# Check status (uses last workflow automatically)
./bin/fleetlift status

# Approve when ready
./bin/fleetlift approve

# Get final result
./bin/fleetlift result
```

The last workflow ID is stored in `~/.fleetlift/last-workflow`. You can always override it by passing `--workflow-id` explicitly.

## Starting Workflows

### From YAML File

```bash
./bin/fleetlift run -f task.yaml
```

### From Flags (Transform Mode)

```bash
./bin/fleetlift run \
  --repo https://github.com/org/repo.git \
  --prompt "Fix the bug in auth.go" \
  --no-approval
```

### Report Mode (Discovery)

```bash
./bin/fleetlift run -f examples/task-report.yaml
```

## Monitoring Workflows

### List Workflows

```bash
# List recent workflows (default: 10)
./bin/fleetlift list

# Show more workflows
./bin/fleetlift list -n 50

# Show all workflows (no limit)
./bin/fleetlift list -n 0

# Filter by status
./bin/fleetlift list --status Running
./bin/fleetlift list --status Completed
./bin/fleetlift list --status Failed
```

### Check Status (Running Workflows)

```bash
# Uses last workflow automatically
./bin/fleetlift status

# Or specify explicitly
./bin/fleetlift status --workflow-id transform-<task-id>
```

### Get Result (Completed Workflows)

```bash
# Uses last workflow automatically
./bin/fleetlift result

# Or specify explicitly
./bin/fleetlift result --workflow-id transform-<task-id>
```

## Report Commands

### View Reports (Report Mode Only)

```bash
# Uses last workflow automatically
./bin/fleetlift reports

# Or specify workflow ID
./bin/fleetlift reports transform-<task-id>

# Full JSON output
./bin/fleetlift reports -o json

# Save to file
./bin/fleetlift reports -o json > report.json

# Only frontmatter (structured data)
./bin/fleetlift reports --frontmatter-only -o json

# Filter to specific target (forEach mode)
./bin/fleetlift reports --target users-api
```

## Workflow Control

### Approve Changes

```bash
# Uses last workflow automatically
./bin/fleetlift approve

# Or specify explicitly
./bin/fleetlift approve --workflow-id transform-<task-id>
```

### Reject Changes

```bash
# Uses last workflow automatically
./bin/fleetlift reject

# Or specify explicitly
./bin/fleetlift reject --workflow-id transform-<task-id>
```

### Cancel Workflow

```bash
# Uses last workflow automatically
./bin/fleetlift cancel

# Or specify explicitly
./bin/fleetlift cancel --workflow-id transform-<task-id>
```

## HITL Steering Commands

These commands enable iterative human-agent collaboration during the approval phase.

> **Note**: Steering is only supported for single-group tasks. For tasks with multiple execution groups, approval is binary (approve/reject only). To use steering, configure a single group or use the default combined strategy.

### View Diff

View changes made by Claude Code while awaiting approval:

```bash
# Uses last workflow automatically
./bin/fleetlift diff

# Show full diff content (not just summary)
./bin/fleetlift diff --full

# Filter to specific file
./bin/fleetlift diff --file auth.go

# JSON output
./bin/fleetlift diff -o json
```

### View Verifier Logs

View verifier output (test results, build output):

```bash
# Uses last workflow automatically
./bin/fleetlift logs

# Filter to specific verifier
./bin/fleetlift logs --verifier test

# JSON output
./bin/fleetlift logs -o json
```

### Steer Agent

Send follow-up prompts to refine Claude's work:

```bash
# Uses last workflow automatically
./bin/fleetlift steer --prompt "Add error handling for the edge case"

# Or specify workflow
./bin/fleetlift steer --workflow-id transform-<task-id> --prompt "Use slog instead of log"
```

After steering, Claude processes the prompt and the workflow returns to awaiting approval. You can check progress with `status`, then view the updated changes with `diff`.

## Important Notes

### Workflow ID Format

Workflow IDs are formatted as `transform-<task-id>-<timestamp>`:

```yaml
# In task file
id: smoke-test

# Actual workflow ID (includes Unix timestamp for uniqueness)
transform-smoke-test-1738512345
```

The timestamp ensures multiple runs of the same task create distinct workflows.

### Workflow ID Memory

After running a workflow, fleetlift remembers it so you don't need to type the ID:

```bash
./bin/fleetlift run -f task.yaml
# Workflow started: transform-smoke-test-1738512345

# All these commands use the last workflow automatically:
./bin/fleetlift status
./bin/fleetlift approve
./bin/fleetlift result
./bin/fleetlift reports
```

The last workflow is stored in `~/.fleetlift/last-workflow`.

### Status vs Result

- **`status`**: Only works for **running** workflows
- **`result`**: Only works for **completed** workflows

```bash
# While running
./bin/fleetlift status

# After completion
./bin/fleetlift result
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
# 1. Start workflow (ID is remembered automatically)
./bin/fleetlift run -f examples/task-agentic.yaml

# 2. Monitor progress
./bin/fleetlift list --status Running

# 3. Check status while running
./bin/fleetlift status

# 4. View in UI
open http://localhost:8233

# 5. Approve (if require_approval: true)
./bin/fleetlift approve

# 6. Get final result
./bin/fleetlift result
```

### Steering Workflow (HITL)

```bash
# 1. Start workflow with approval required
./bin/fleetlift run -f task.yaml
# (task.yaml has require_approval: true)

# 2. Wait for awaiting_approval status
./bin/fleetlift status
# Status: awaiting_approval

# 3. View what changed
./bin/fleetlift diff
# Repository: my-service
#   Summary: 3 files changed, +45, -12
#   modified src/auth.go (+30/-5)
#   modified src/util.go (+15/-7)

# 4. View test results
./bin/fleetlift logs
# [PASS] my-service:test (exit code: 0)

# 5. Request changes
./bin/fleetlift steer --prompt "Also add validation for empty strings"

# 6. Wait for completion, check updated diff
./bin/fleetlift status
./bin/fleetlift diff
# Now shows 4 files with additional validation

# 7. Approve final changes
./bin/fleetlift approve
```

### Report Mode Workflow

```bash
# 1. Start discovery
./bin/fleetlift run -f examples/task-report.yaml

# 2. Wait for completion (or check status)
./bin/fleetlift status

# 3. View reports
./bin/fleetlift reports

# 4. Export to JSON
./bin/fleetlift reports -o json > results.json

# 5. Process structured data
cat results.json | jq '.[] | .frontmatter'
```

### forEach Workflow

```bash
# 1. Start forEach analysis
./bin/fleetlift run -f examples/task-foreach.yaml

# 2. Get all results
./bin/fleetlift reports -o json

# 3. Filter to specific target
./bin/fleetlift reports --target users-api -o json
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
# Extract all security scores (uses last workflow)
./bin/fleetlift reports -o json | jq '.[] | .frontmatter.score'

# Find high-severity issues
./bin/fleetlift reports -o json | \
  jq '.[] | .frontmatter.issues[] | select(.severity == "high")'

# Group by repository
./bin/fleetlift reports -o json | jq 'group_by(.repository)'
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
# List all failed workflows (no limit)
./bin/fleetlift list --status Failed -n 0

# Cancel all running workflows (careful!)
./bin/fleetlift list --status Running -n 0 | \
  awk '{print $1}' | \
  xargs -I {} ./bin/fleetlift cancel --workflow-id {}
```

## See Also

- [Task File Reference](TASK_FILE_REFERENCE.md) - YAML specification
- [Examples](examples/) - Example workflows
- [Troubleshooting](TROUBLESHOOTING.md) - Common issues
