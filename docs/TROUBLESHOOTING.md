# Troubleshooting

Common issues and solutions when using Fleetlift.

## Workflow Issues

### "Workflow is running but nothing happens"

**Symptoms:**
- Workflow shows "Running" status
- No progress in Temporal UI
- Claude Code activity never starts

**Solutions:**

1. **Check if worker is running:**
   ```bash
   ps aux | grep bin/worker
   # Should show a running process
   ```

2. **Verify API key is set:**
   ```bash
   # Worker needs ANTHROPIC_API_KEY for agentic mode
   export ANTHROPIC_API_KEY=sk-ant-...
   make run-worker
   ```

3. **Check worker logs for errors:**
   ```bash
   # Look for configuration warnings or errors
   make run-worker
   ```

4. **Verify Docker is running (for sandbox):**
   ```bash
   docker ps
   # Should list running containers
   ```

5. **Check Temporal UI:**
   ```bash
   open http://localhost:8233
   # Navigate to workflow > History
   # Look for errors or pending activities
   ```

### "Status shows 'running' for completed workflow"

**Problem:**
The `status` command only works for **running** workflows. Use `result` for completed ones.

**Solution:**
```bash
# Wrong (for completed workflow)
./bin/fleetlift status --workflow-id transform-my-task

# Correct
./bin/fleetlift result --workflow-id transform-my-task
```

### "Workflow waiting for approval indefinitely"

**Symptoms:**
- Report mode task stuck at approval step
- Transform mode needs approval but you didn't realize

**Solutions:**

1. **For report mode, disable approval:**
   ```yaml
   mode: report
   require_approval: false  # Add this
   ```

2. **Approve the workflow:**
   ```bash
   ./bin/fleetlift approve --workflow-id transform-<task-id>
   ```

3. **Check approval status in Temporal UI:**
   - Navigate to workflow
   - Look for "Waiting for signal: approval-decision"

### "Activity not registered" error

**Symptoms:**
- Error: "activity type not registered"
- Workflow fails immediately

**Solutions:**

1. **Restart the worker:**
   ```bash
   # Kill old worker
   pkill -f bin/worker

   # Start fresh worker
   make run-worker
   ```

2. **Verify worker binary is up to date:**
   ```bash
   # Rebuild
   make build

   # Restart worker
   make run-worker
   ```

3. **Check activity registration in worker logs:**
   - Should see: "Registered activity: ActivityName"

## Docker / Sandbox Issues

### "Cannot connect to Docker daemon"

**Symptoms:**
- Error: "Cannot connect to the Docker daemon"
- Sandbox creation fails

**Solutions:**

1. **Start Docker Desktop:**
   ```bash
   # macOS
   open -a Docker

   # Verify
   docker ps
   ```

2. **Check Docker socket permissions:**
   ```bash
   ls -la /var/run/docker.sock
   # Should be readable by your user
   ```

### "Sandbox container exits immediately"

**Symptoms:**
- Container starts but exits with code 1
- No output from Claude Code

**Solutions:**

1. **Check sandbox image exists:**
   ```bash
   docker images | grep claude-code-sandbox
   ```

2. **Build sandbox image:**
   ```bash
   make sandbox-build
   ```

3. **Test sandbox locally:**
   ```bash
   docker run -it claude-code-sandbox:latest /bin/bash
   ```

4. **Check logs in Temporal UI:**
   - Activity details show container output

## Claude Code Issues

### "claude code failed: exit code -1" with empty output

**Symptoms:**
- Workflow status is `Completed` (from Temporal's perspective) but result shows `"status":"failed"`
- Error: `transformation failed: claude code failed: exit code -1: \nOutput: `
- Workflow ran for ~14 minutes before failing (activity timeout, not an immediate failure)

**Cause:**
The `ANTHROPIC_API_KEY` was not set in the worker's environment. The container starts and runs Claude Code, which outputs "Not logged in · Please run /login" and exits. The empty `Output:` field is the giveaway.

**Solution:**
Create a `.env` file in the project root with your API key:
```bash
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env
```
Then restart the worker (`scripts/integration/start-worker.sh` loads `.env` automatically).

**Fast check:**
```bash
# Worker logs at startup will warn if key is missing:
grep "CONFIG WARNING.*ANTHROPIC" /tmp/fleetlift-worker.log
```

### "Claude Code exceeds iteration limit"

**Symptoms:**
- Error: "max_iterations exceeded"
- Workflow fails after many iterations

**Solutions:**

1. **Increase iteration limit:**
   ```yaml
   execution:
     agentic:
       limits:
         max_iterations: 100  # Increase from default 50
   ```

2. **Make prompt more specific:**
   - Provide exact file paths
   - Give clear success criteria
   - Add examples of desired output

3. **Check for infinite loops:**
   - Review Claude Code conversation in Temporal UI
   - Look for repeated failed attempts

### "Verifier fails unexpectedly"

**Symptoms:**
- Claude Code completes successfully
- Verifier command fails
- No PR created

**Solutions:**

1. **Check verifier command:**
   ```yaml
   verifiers:
     - name: test
       command: [go, test, ./...]  # Verify command is correct
   ```

2. **Test verifier locally:**
   ```bash
   cd /path/to/repo
   go test ./...
   # Debug any failures
   ```

3. **Add setup commands:**
   ```yaml
   repositories:
     - url: https://github.com/org/repo.git
       setup:
         - go mod download  # Install dependencies first
   ```

4. **View verifier output:**
   - Check Temporal UI > Activity > Output
   - Shows stdout/stderr from verifier

## API / Authentication Issues

### "Invalid API key" errors

**Symptoms:**
- Error: "invalid API key"
- Claude Code activity fails immediately

**Solutions:**

1. **Verify API key format:**
   ```bash
   echo $ANTHROPIC_API_KEY
   # Should start with sk-ant-
   ```

2. **Re-export API key:**
   ```bash
   export ANTHROPIC_API_KEY=sk-ant-your-key-here
   make run-worker
   ```

3. **Check key has not expired:**
   - Log into Anthropic console
   - Verify key is active

### "GitHub authentication failed"

**Symptoms:**
- Error creating PR: "authentication required"
- Git push fails

**Solutions:**

1. **Set GitHub token:**
   ```bash
   export GITHUB_TOKEN=ghp_...
   make run-worker
   ```

2. **Verify token has correct scopes:**
   - Needs: `repo` (full control)
   - Create at: https://github.com/settings/tokens

3. **Check repository permissions:**
   - Token owner must have write access
   - For org repos, enable SSO if required

## Configuration Issues

### "Task file validation failed"

**Symptoms:**
- CLI rejects task file before starting
- Error messages about missing fields

**Solutions:**

1. **Check required fields:**
   ```yaml
   version: 1           # Required
   id: my-task         # Required
   title: "My task"    # Required
   ```

2. **Validate YAML syntax:**
   ```bash
   # Use yamllint or similar
   yamllint task.yaml
   ```

3. **Check field types:**
   ```yaml
   timeout: 30m        # Correct (Go duration string: "30m", "1h", "90s")
   ```

### "Invalid mode" error

**Problem:**
Mode must be exactly `transform` or `report`.

**Solution:**
```yaml
mode: transform  # Correct
# NOT: transforms, transformation, reporting, discover
```

## Performance Issues

### "Workflow takes too long"

**Symptoms:**
- Exceeds timeout
- Individual repos take 20+ minutes

**Solutions:**

1. **Increase timeout:**
   ```yaml
   timeout: 60m  # Increase from 30m
   ```

2. **Use deterministic mode for simple changes:**
   ```yaml
   execution:
     deterministic:  # Faster than agentic
       image: your-tool:latest
   ```

3. **Enable parallel execution:**
   ```bash
   # Use --parallel flag to auto-generate one group per repo
   ./bin/fleetlift run -f task.yaml --parallel
   ```
   Or use explicit groups with `max_parallel` in your task YAML.

4. **Simplify the prompt:**
   - Break into smaller tasks
   - Focus on specific changes

### "Too many API calls / rate limited"

**Symptoms:**
- Error: "rate limit exceeded"
- Slow Claude Code responses

**Solutions:**

1. **Reduce parallelism:**
   ```yaml
   max_parallel: 1  # Sequential execution
   ```

2. **Add delays between repos:**
   - Use sequential mode
   - Temporal handles retries automatically

3. **Contact Anthropic for rate limit increase**

## Temporal Issues

### "Connection refused to Temporal server"

**Symptoms:**
- Error: "connection refused localhost:7233"
- CLI can't reach Temporal

**Solutions:**

1. **Start Temporal dev server:**
   ```bash
   make temporal-dev
   ```

2. **Verify Temporal is running:**
   ```bash
   temporal server info
   # Should show server details
   ```

3. **Check port is not in use:**
   ```bash
   lsof -i :7233
   # Should show Temporal process
   ```

### "Workflow execution history too large"

**Symptoms:**
- Error: "history size limit exceeded"
- Very long-running workflows fail

**Solutions:**

1. **Break into smaller tasks:**
   - Instead of 50 repos, do 10 at a time

2. **Use continue-as-new pattern:**
   - For very large batches
   - Advanced: requires code changes

3. **Clean up old workflows:**
   ```bash
   # Use Temporal CLI to clean old executions
   temporal workflow delete --query "StartTime < '2024-01-01'"
   ```

## Report Mode Issues

### "No reports found"

**Symptoms:**
- `./bin/fleetlift reports` returns empty
- Workflow completed but no data

**Solutions:**

1. **Verify mode is 'report':**
   ```yaml
   mode: report  # Required for reports command
   ```

2. **Check Claude Code created REPORT.md:**
   - View activity output in Temporal UI
   - Look for /workspace/REPORT.md

3. **Verify output schema validation passed:**
   - Check for validation errors in logs

### "JSON Schema validation failed"

**Symptoms:**
- Error: "frontmatter does not match schema"
- Report mode workflow fails

**Solutions:**

1. **Check schema matches frontmatter:**
   ```yaml
   schema:
     type: object
     required: [field1]  # Field1 must exist in YAML frontmatter
   ```

2. **Make prompt more explicit:**
   ```yaml
   prompt: |
     Write REPORT.md with YAML frontmatter containing exactly:
     - field1: string
     - field2: integer
   ```

3. **Test schema locally:**
   - Use JSON Schema validator
   - Verify example frontmatter passes

## `fleetlift stream` Issues

### "unknown queryType get_sandbox_id" on multi-repo workflow

**Symptoms:**
```
Error: failed to get sandbox ID: failed to query sandbox ID: unknown queryType get_sandbox_id.
KnownQueryTypes=[__stack_trace __open_sessions get_execution_progress]
```

**Cause:**
`stream` was given the top-level `Transform` workflow ID (e.g. `transform-task-XXXX-YYYY`). The `get_sandbox_id` query is only registered on `TransformV2` *execution* child workflows. Multi-repo tasks use a parent → group → exec hierarchy.

**Solution:**
Use `--group` to target a specific repo's agent:
```bash
# The CLI will list available groups if you omit --group:
./bin/fleetlift stream --workflow-id transform-task-XXXX-YYYY
# Output:
#   fleetlift stream --workflow-id transform-task-XXXX-YYYY --group express
#   fleetlift stream --workflow-id transform-task-XXXX-YYYY --group fastify
#   fleetlift stream --workflow-id transform-task-XXXX-YYYY --group koa

# Then pick one:
./bin/fleetlift stream --workflow-id transform-task-XXXX-YYYY --group express
```

For single-repo tasks, the CLI auto-selects the only group.

### "dial tcp: lookup host.docker.internal: no such host"

**Symptoms:**
```
Error: stream error: execd tail /workspace/.fleetlift/agent.log:
  Post "http://host.docker.internal:XXXXX/proxy/44772/command": dial tcp: lookup host.docker.internal: no such host
```

**Cause:**
With `OPEN_SANDBOX_USE_SERVER_PROXY=false` (the old default), the OpenSandbox lifecycle server returns a direct container endpoint using `host.docker.internal` as the hostname. This doesn't resolve reliably on the macOS host.

**Solution (already fixed in CLI):**
The `stream` command now defaults to server proxy mode (`useServerProxy=true`), which routes through `localhost:8090` instead. If you see this error with a custom build, set:
```bash
OPEN_SANDBOX_USE_SERVER_PROXY=true ./bin/fleetlift stream --workflow-id ...
```
Or explicitly opt out (if you know direct access works in your environment):
```bash
OPEN_SANDBOX_USE_SERVER_PROXY=false ./bin/fleetlift stream --workflow-id ...
```

## Getting More Help

### View Detailed Execution

```bash
# Open Temporal UI
open http://localhost:8233

# Navigate to:
# Namespaces > default > Workflows > transform-<task-id>

# Review:
# - Event History (every step)
# - Activity inputs/outputs
# - Error messages
# - Timeline
```

### Enable Debug Logging

```bash
# Start worker with verbose logging
LOG_LEVEL=debug make run-worker
```

### Check Worker Health

```bash
# Worker should show:
# - Connected to Temporal
# - Registered activities
# - Polling for tasks

make run-worker
# Look for: "Worker started successfully"
```

### Common Log Messages

**Good signs:**
```
INFO: Connected to Temporal server
INFO: Registered activity: CloneRepositoryActivity
INFO: Worker started, polling for tasks
```

**Warning signs:**
```
WARN: Failed to connect to Docker
ERROR: Activity registration failed
ERROR: Invalid API key
```

## Still Stuck?

1. **Check example files:**
   - Use provided examples as templates
   - Verify they work before customizing

2. **Review documentation:**
   - [CLI Reference](CLI_REFERENCE.md)
   - [Task File Reference](TASK_FILE_REFERENCE.md)
   - [Examples](examples/)

3. **File an issue:**
   - https://github.com/tinkerloft/fleetlift/issues
   - Include: task file, error logs, Temporal workflow ID

4. **Check Temporal docs:**
   - https://docs.temporal.io
   - For Temporal-specific issues
