# MCP Sidecar Integration Test — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify the full MCP sidecar round-trip in a real sandbox: provisioning, sidecar startup, all 7 API endpoints, and optionally Claude Code MCP discovery.

**Architecture:** New `mcp-test` builtin workflow with a shell step that curls MCP endpoints from inside a sandbox, plus an optional shell-wrapper step that invokes Claude Code. A test runner script submits the workflow, polls Temporal, and asserts DB state.

**Tech Stack:** Bash (workflow prompts + test script), Go (provision.go token injection change), YAML (workflow template)

**Spec:** `docs/superpowers/specs/2026-03-16-mcp-integration-test-design.md`

---

## Chunk 1: Token Injection in ProvisionSandbox

### Task 1: Export FLEETLIFT_MCP_TOKEN to profile.d

Currently `ProvisionSandbox` only passes the token as an inline env var on the nohup command. The shell test step needs it available in the sandbox environment.

**Files:**
- Modify: `internal/activity/provision.go:157-162`
- Modify: `internal/activity/provision_test.go` (TestProvisionSandbox_MCPSetup)

- [x] **Step 1: Write failing test**

Add assertion to `TestProvisionSandbox_MCPSetup` in `internal/activity/provision_test.go` that verifies the profile.d exec command includes `FLEETLIFT_MCP_TOKEN`:

```go
// After existing assertions in TestProvisionSandbox_MCPSetup, add:
// Verify profile.d write includes both PORT and TOKEN
foundTokenExport := false
for _, cmd := range sb.execCmds {
    if strings.Contains(cmd, "FLEETLIFT_MCP_TOKEN") && strings.Contains(cmd, "profile.d") {
        foundTokenExport = true
    }
}
assert.True(t, foundTokenExport, "expected profile.d write to include FLEETLIFT_MCP_TOKEN export")
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity/ -run TestProvisionSandbox_MCPSetup -v`
Expected: FAIL — profile.d write currently only exports `FLEETLIFT_MCP_PORT`

- [x] **Step 3: Implement — add token export to profile.d write**

In `internal/activity/provision.go`, replace the profile.d write block (lines 157-162):

```go
		// Inject MCP port and token into sandbox env so the agent runner and test steps can use them.
		profileCmd := fmt.Sprintf(
			"printf 'export FLEETLIFT_MCP_PORT=%s\nexport FLEETLIFT_MCP_TOKEN=%s\n' >> /etc/profile.d/fleetlift-mcp.sh",
			shellquote.Quote(mcpPort), shellquote.Quote(mcpToken),
		)
		if _, _, err := a.Sandbox.Exec(ctx, sandboxID, profileCmd, "/"); err != nil {
			_ = a.Sandbox.Kill(ctx, sandboxID)
			return "", fmt.Errorf("persist MCP env in sandbox: %w", err)
		}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/activity/ -run TestProvisionSandbox_MCPSetup -v`
Expected: PASS

- [x] **Step 5: Run full test suite + lint**

Run: `go test ./... && make lint`
Expected: All pass, no lint errors

- [x] **Step 6: Commit**

```bash
git add internal/activity/provision.go internal/activity/provision_test.go
git commit -m "feat: export FLEETLIFT_MCP_TOKEN to sandbox profile.d for integration tests"
```

---

## Chunk 2: Workflow Template

### Task 2: Create mcp-test.yaml workflow template

**Files:**
- Create: `internal/template/workflows/mcp-test.yaml`

- [ ] **Step 1: Create the workflow template**

Create `internal/template/workflows/mcp-test.yaml`:

```yaml
version: 1
id: mcp-test
title: MCP Sidecar Test
description: Diagnostic workflow — verifies MCP sidecar provisioning, all 7 backend API endpoints, and optionally Claude Code MCP discovery.
tags:
  - test
  - diagnostic
  - mcp
parameters:
  - name: include_agent_step
    type: string
    required: false
    default: "false"
    description: "Set to 'true' to run the optional Claude Code agent step (costs AI tokens)"
steps:
  - id: verify_mcp_endpoints
    title: Verify MCP sidecar and API endpoints
    sandbox_group: mcp-test
    mode: report
    execution:
      agent: shell
      prompt: |
        set -euo pipefail

        # ── Load MCP env ──
        . /etc/profile.d/fleetlift-mcp.sh 2>/dev/null || true

        if [ -z "${FLEETLIFT_MCP_PORT:-}" ]; then
          echo "FAIL: FLEETLIFT_MCP_PORT not set — ProvisionSandbox did not inject port"
          exit 1
        fi
        if [ -z "${FLEETLIFT_MCP_TOKEN:-}" ]; then
          echo "FAIL: FLEETLIFT_MCP_TOKEN not set — ProvisionSandbox did not inject token"
          exit 1
        fi

        API="http://host.docker.internal:8080/api/mcp"
        AUTH="Authorization: Bearer $FLEETLIFT_MCP_TOKEN"
        PASS=0
        FAIL=0

        check() {
          local name="$1" expected="$2" actual="$3"
          if [ "$actual" = "$expected" ]; then
            echo "PASS: $name (HTTP $actual)"
            PASS=$((PASS + 1))
          else
            echo "FAIL: $name — expected HTTP $expected, got $actual"
            FAIL=$((FAIL + 1))
          fi
        }

        # ── Sidecar health check (retry 5x) ──
        HEALTHY=false
        for i in 1 2 3 4 5; do
          if curl -sf "http://localhost:$FLEETLIFT_MCP_PORT/health" | grep -q ok; then
            HEALTHY=true
            break
          fi
          sleep 1
        done
        if [ "$HEALTHY" = "true" ]; then
          echo "PASS: sidecar health check"
          PASS=$((PASS + 1))
        else
          echo "FAIL: sidecar health check — not responding after 5s"
          exit 1
        fi

        # ── 1. POST /progress (call early while step is active) ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' -X POST \
          -H "$AUTH" -H "Content-Type: application/json" \
          -d '{"percentage":25,"message":"mcp-test-shell"}' \
          "$API/progress")
        check "POST /progress" "200" "$CODE"

        # ── 2. POST /artifacts ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' -X POST \
          -H "$AUTH" -H "Content-Type: application/json" \
          -d '{"name":"mcp-test-shell.txt","content":"hello from shell"}' \
          "$API/artifacts")
        check "POST /artifacts" "201" "$CODE"
        cat /tmp/r.json | grep -q artifact_id && echo "  artifact_id present" || echo "  WARN: artifact_id missing"

        # ── 3. GET /run ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' \
          -H "$AUTH" "$API/run")
        check "GET /run" "200" "$CODE"
        cat /tmp/r.json | grep -q run_id && echo "  run_id present" || echo "  WARN: run_id missing"

        # ── 4. POST /knowledge ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' -X POST \
          -H "$AUTH" -H "Content-Type: application/json" \
          -d '{"type":"pattern","summary":"mcp-test-learning","details":"integration test item"}' \
          "$API/knowledge")
        check "POST /knowledge" "201" "$CODE"

        # ── 5. GET /knowledge/search ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' \
          -H "$AUTH" "$API/knowledge/search?q=mcp-test&max=5")
        check "GET /knowledge/search" "200" "$CODE"

        # ── 6. GET /knowledge ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' \
          -H "$AUTH" "$API/knowledge?max=5")
        check "GET /knowledge" "200" "$CODE"

        # ── 7. GET /steps/nonexistent/output (expect 404) ──
        CODE=$(curl -s -o /tmp/r.json -w '%{http_code}' \
          -H "$AUTH" "$API/steps/nonexistent/output")
        check "GET /steps/nonexistent/output" "404" "$CODE"

        # ── Summary ──
        echo ""
        echo "Results: $PASS passed, $FAIL failed"
        if [ "$FAIL" -gt 0 ]; then
          exit 1
        fi
        echo "mcp-endpoints: ALL PASSED"

  - id: agent_uses_mcp
    title: Agent uses MCP tools
    depends_on:
      - verify_mcp_endpoints
    sandbox_group: mcp-test
    mode: report
    execution:
      agent: shell
      prompt: |
        set -euo pipefail

        if [ "{{ .Params.include_agent_step }}" != "true" ]; then
          echo "agent step skipped (include_agent_step != true)"
          exit 0
        fi

        # Source MCP env
        . /etc/profile.d/fleetlift-mcp.sh 2>/dev/null || true

        if [ -z "${FLEETLIFT_MCP_PORT:-}" ]; then
          echo "FAIL: FLEETLIFT_MCP_PORT not set"
          exit 1
        fi

        # Write MCP config for Claude Code discovery
        mkdir -p /workspace
        printf '{"mcpServers":{"fleetlift":{"type":"sse","url":"http://localhost:%s/sse"}}}' "$FLEETLIFT_MCP_PORT" > /workspace/.mcp.json

        cd /workspace
        claude -p "You have MCP tools available from the fleetlift server. Do these in order: (1) Use the progress.update tool to report 75 percent progress with message 'agent-mcp-test'. (2) Use the artifact.create tool to create an artifact named 'mcp-test-agent.txt' with content 'hello from agent mcp'. (3) Print DONE." \
          --output-format stream-json --dangerously-skip-permissions --max-turns 10 2>&1 || true

        echo "agent-mcp-step-complete"
```

- [ ] **Step 2: Verify template loads**

Run: `go test ./internal/template/ -v`
Expected: PASS (builtin provider loads all YAML templates including the new one)

- [ ] **Step 3: Commit**

```bash
git add internal/template/workflows/mcp-test.yaml
git commit -m "feat: add mcp-test workflow template for integration testing"
```

---

## Chunk 3: Test Runner Script

### Task 3: Create run-mcp-test.sh

**Files:**
- Create: `scripts/integration/run-mcp-test.sh`

- [ ] **Step 1: Create the test runner script**

Create `scripts/integration/run-mcp-test.sh`:

```bash
#!/usr/bin/env bash
# Run the mcp-test workflow and verify MCP sidecar integration.
# Exercises: sidecar provisioning → health check → 7 API endpoints → DB state.
# Usage: run-mcp-test.sh [--with-agent]
set -euo pipefail
source "$(dirname "$0")/dev-env.sh"
cd "$PROJECT_ROOT"

# ── Parse flags ──────────────────────────────────────────────────────────────
INCLUDE_AGENT="false"
TIMEOUT=60
if [[ "${1:-}" == "--with-agent" ]]; then
  INCLUDE_AGENT="true"
  TIMEOUT=120
fi

# ── Check prerequisites ─────────────────────────────────────────────────────
if [[ ! -f bin/fleetlift-mcp ]]; then
  echo "ERROR: bin/fleetlift-mcp not found."
  echo "  Run: make mcp-sidecar"
  exit 1
fi

if ! file bin/fleetlift-mcp | grep -q ELF; then
  echo "ERROR: bin/fleetlift-mcp is not a Linux ELF binary (sandbox requires linux/amd64)."
  echo "  Run: make mcp-sidecar"
  exit 1
fi

export FLEETLIFT_MCP_BINARY_PATH="$PROJECT_ROOT/bin/fleetlift-mcp"
echo "MCP binary: $FLEETLIFT_MCP_BINARY_PATH"

# Check worker is running
if [[ -f "$WORKER_PIDFILE" ]] && kill -0 "$(cat "$WORKER_PIDFILE")" 2>/dev/null; then
  echo "Worker PID: $(cat "$WORKER_PIDFILE")"
else
  echo "ERROR: Worker is not running."
  echo "  Run: scripts/integration/start.sh"
  exit 1
fi

# ── Generate JWT ──────────────────────────────────────────────────────────────
TEAM_ID=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT id FROM teams LIMIT 1" 2>/dev/null | tr -d ' \n')
USER_ID=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT id FROM users LIMIT 1" 2>/dev/null | tr -d ' \n')

if [[ -z "$TEAM_ID" || -z "$USER_ID" ]]; then
  echo "ERROR: No team or user found in database."
  echo "  Ensure the database is seeded with at least one team and user."
  exit 1
fi

TMPDIR_JWT=$(mktemp -d)
TMPJWT="$TMPDIR_JWT/genjwt.go"
cat > "$TMPJWT" <<'GOEOF'
package main
import (
	"fmt"; "os"; "time"
	"github.com/golang-jwt/jwt/v5"
)
type Claims struct {
	UserID    string            `json:"user_id"`
	TeamRoles map[string]string `json:"team_roles"`
	jwt.RegisteredClaims
}
func main() {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:    os.Args[1],
		TeamRoles: map[string]string{os.Args[2]: "admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	s, _ := t.SignedString([]byte(os.Args[3]))
	fmt.Print(s)
}
GOEOF
JWT=$(go run "$TMPJWT" "$USER_ID" "$TEAM_ID" "$JWT_SECRET")
rm -rf "$TMPDIR_JWT"

API="http://localhost:8080"
AUTH=(-H "Authorization: Bearer $JWT" -H "X-Team-ID: $TEAM_ID")

# ── Verify server is up ──────────────────────────────────────────────────────
if ! curl -sf "${AUTH[@]}" "$API/api/workflows" > /dev/null 2>&1; then
  echo "ERROR: Server not responding or auth failed."
  echo "  Run: scripts/integration/start.sh --build"
  exit 1
fi

# ── Submit mcp-test workflow ─────────────────────────────────────────────────
echo "Submitting mcp-test workflow (include_agent_step=$INCLUDE_AGENT)..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  "${AUTH[@]}" \
  -H "Content-Type: application/json" \
  -d "{
    \"workflow_id\": \"mcp-test\",
    \"parameters\": {
      \"include_agent_step\": \"$INCLUDE_AGENT\"
    }
  }" \
  "$API/api/runs")

HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" != "201" ]]; then
  echo "ERROR: Failed to start workflow (HTTP $HTTP_CODE): $BODY"
  exit 1
fi

RUN_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
TEMPORAL_ID=$(echo "$BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['temporal_id'])")
echo "Run ID:      $RUN_ID"
echo "Temporal ID: $TEMPORAL_ID"

# ── Wait for completion ───────────────────────────────────────────────────────
echo "Waiting for workflow to complete (timeout: ${TIMEOUT}s)..."
STATUS=""
for i in $(seq 1 $((TIMEOUT / 2))); do
  DESCRIBE=$(temporal workflow describe \
    --workflow-id "$TEMPORAL_ID" \
    --address "$TEMPORAL_ADDRESS" 2>/dev/null || echo "")

  if echo "$DESCRIBE" | grep -q "Status.*COMPLETED"; then
    STATUS="COMPLETED"
    break
  elif echo "$DESCRIBE" | grep -qE "Status.*(FAILED|TERMINATED|CANCELED|TIMED_OUT)"; then
    STATUS=$(echo "$DESCRIBE" | grep "Status" | awk '{print $NF}')
    echo "FAILED: Workflow ended with status $STATUS"
    temporal workflow show --workflow-id "$TEMPORAL_ID" --address "$TEMPORAL_ADDRESS" 2>&1 | tail -20
    exit 1
  fi

  sleep 2
done

if [[ "${STATUS:-}" != "COMPLETED" ]]; then
  echo "TIMEOUT: Workflow did not complete in ${TIMEOUT} seconds"
  exit 1
fi

# ── Verify results ────────────────────────────────────────────────────────────
echo ""
echo "=== DB Verification ==="
OK=true

# Check step statuses
STEP_RESULTS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c "
SELECT json_agg(json_build_object(
  'step_id', step_id,
  'status', status
)) FROM step_runs WHERE run_id = '$RUN_ID'" 2>/dev/null | tr -d '\n ')

echo "$STEP_RESULTS" | python3 -c "
import sys, json
steps = json.load(sys.stdin)
for s in steps:
    print(f\"  {s['step_id']}: {s['status']}\")
"

# Check shell step completed
SHELL_STATUS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT status FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'verify_mcp_endpoints'" 2>/dev/null | tr -d ' \n')
if [[ "$SHELL_STATUS" == "complete" ]]; then
  echo "PASS: verify_mcp_endpoints completed"
else
  echo "FAIL: verify_mcp_endpoints status=$SHELL_STATUS"
  OK=false
fi

# Check artifact was created
ARTIFACT_COUNT=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID' AND a.name = 'mcp-test-shell.txt'" 2>/dev/null | tr -d ' \n')
if [[ "$ARTIFACT_COUNT" -ge 1 ]]; then
  echo "PASS: artifact 'mcp-test-shell.txt' found"
else
  echo "FAIL: artifact 'mcp-test-shell.txt' not found"
  OK=false
fi

# Check progress was written (may be overwritten by final output — accept either)
PROGRESS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT output->'progress'->>'percentage' FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'verify_mcp_endpoints'" 2>/dev/null | tr -d ' \n')
if [[ "$PROGRESS" == "25" ]]; then
  echo "PASS: progress percentage=25 found in step output"
else
  echo "INFO: progress percentage not found in final output (may have been overwritten — POST /progress returned 200 during execution)"
fi

# Check knowledge item was created
KNOWLEDGE_COUNT=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
  "SELECT count(*) FROM knowledge_items WHERE team_id = '$TEAM_ID' AND summary = 'mcp-test-learning'" 2>/dev/null | tr -d ' \n')
if [[ "$KNOWLEDGE_COUNT" -ge 1 ]]; then
  echo "PASS: knowledge item 'mcp-test-learning' found"
else
  echo "FAIL: knowledge item 'mcp-test-learning' not found"
  OK=false
fi

# ── Agent step verification (if enabled) ──────────────────────────────────────
if [[ "$INCLUDE_AGENT" == "true" ]]; then
  AGENT_STATUS=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
    "SELECT status FROM step_runs WHERE run_id = '$RUN_ID' AND step_id = 'agent_uses_mcp'" 2>/dev/null | tr -d ' \n')
  if [[ "$AGENT_STATUS" == "complete" ]]; then
    echo "PASS: agent_uses_mcp completed"
  else
    echo "FAIL: agent_uses_mcp status=$AGENT_STATUS"
    OK=false
  fi

  AGENT_ARTIFACT=$(docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -t -c \
    "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID' AND a.name = 'mcp-test-agent.txt'" 2>/dev/null | tr -d ' \n')
  if [[ "$AGENT_ARTIFACT" -ge 1 ]]; then
    echo "PASS: agent artifact 'mcp-test-agent.txt' found"
  else
    echo "FAIL: agent artifact 'mcp-test-agent.txt' not found"
    OK=false
  fi
fi

# ── Cleanup test data ─────────────────────────────────────────────────────────
docker exec fleetlift-postgres-1 psql -U fleetlift -d fleetlift -c \
  "DELETE FROM knowledge_items WHERE summary = 'mcp-test-learning'" 2>/dev/null || true

# ── Final result ──────────────────────────────────────────────────────────────
echo ""
if $OK; then
  echo "mcp-test: PASSED"
else
  echo "mcp-test: FAILED"
  exit 1
fi
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/integration/run-mcp-test.sh
```

- [ ] **Step 3: Commit**

```bash
git add scripts/integration/run-mcp-test.sh
git commit -m "feat: add MCP sidecar integration test runner script"
```

---

## Chunk 4: Verification & Docs

### Task 4: Update CLAUDE.md and verify

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add mcp-test to CLAUDE.md local server operations**

Add to the scripts list in CLAUDE.md under "Local Server Operations":

```
- `scripts/integration/run-mcp-test.sh [--with-agent]` — run MCP sidecar integration test
```

- [ ] **Step 2: Build and run the test (manual verification)**

```bash
make mcp-sidecar
export FLEETLIFT_MCP_BINARY_PATH="$(pwd)/bin/fleetlift-mcp"
scripts/integration/restart.sh
scripts/integration/run-mcp-test.sh
```

Expected: All checks PASS.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add mcp-test script to CLAUDE.md operations list"
```

- [ ] **Step 4: Update implementation plan with completion status**

Mark all tasks complete in this plan document.
