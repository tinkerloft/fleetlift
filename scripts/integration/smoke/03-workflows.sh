#!/usr/bin/env bash
# 03-workflows.sh — Built-in workflow E2E smoke tests
source "$(cd "$(dirname "$0")/.." && pwd)/smoke-lib.sh"

# ── Credential detection ─────────────────────────────────────────
HAS_GITHUB_TOKEN=false
if has_credential "GITHUB_TOKEN"; then
  HAS_GITHUB_TOKEN=true
fi

# Test repository (public, small) for workflows that need a repo
TEST_REPO="https://github.com/tinkerloft/fleetlift"

# ── Helper: run a workflow and verify completion ─────────────────
# Usage: run_workflow <test_name> <workflow_id> '<json_params>' [timeout_seconds]
run_workflow() {
  local test_name="$1" workflow_id="$2" params="$3" timeout="${4:-120}"

  submit_run "$workflow_id" "$params"
  if [[ "$HTTP_STATUS" != "201" ]]; then
    fail "$test_name — submit failed (HTTP $HTTP_STATUS)"
    local body_preview="${HTTP_BODY:0:500}"
    [[ -n "$body_preview" ]] && printf "    Body: %s\n" "$body_preview"
    return
  fi
  pass "$test_name — submitted (run=$RUN_ID)"

  if wait_for_run "$timeout"; then
    pass "$test_name — completed"
  else
    fail "$test_name — $WORKFLOW_STATUS (run=$RUN_ID, temporal=$TEMPORAL_ID)"
    printf "    Hint: temporal workflow show --workflow-id %s --address %s\n" "$TEMPORAL_ID" "$TEMPORAL_ADDRESS"
  fi
}

# ── Helper: verify all steps in a run completed ──────────────────
verify_steps() {
  local test_name="$1" run_id="$2"
  local failed_steps
  failed_steps=$(fl_sql "SELECT step_id FROM step_runs WHERE run_id = '$run_id' AND status != 'complete'" | tr -d ' ')
  if [[ -z "$failed_steps" ]]; then
    pass "$test_name — all steps complete"
  else
    fail "$test_name — incomplete steps: $failed_steps"
  fi
}

# ══════════════════════════════════════════════════════════════════
# TIER 1: No external credentials required
# ══════════════════════════════════════════════════════════════════

section "Tier 1: Diagnostic Workflows (no credentials)"

# ── sandbox-test ──────────────────────────────────────────────────
run_workflow "sandbox-test" "sandbox-test" '{"duration":5,"command2":"echo smoke-ok"}'
if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
  verify_steps "sandbox-test" "$RUN_ID"

  # Verify step 2 received output from step 1
  S2_OUTPUT=$(step_output "$RUN_ID" "use_output")
  assert_contains "sandbox-test step2 got step1 output" "${S2_OUTPUT:-}" "smoke-ok"
fi

# ── profile-test ──────────────────────────────────────────────────
run_workflow "profile-test" "profile-test" '{}' 180
if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
  verify_steps "profile-test" "$RUN_ID"
fi

# ── mcp-test ──────────────────────────────────────────────────────
run_workflow "mcp-test" "mcp-test" '{}' 180
if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
  MCP_STEP1=$(step_status "$RUN_ID" "verify_mcp_endpoints")
  assert_equals "mcp-test shell step" "complete" "$MCP_STEP1"

  ARTIFACT_COUNT=$(fl_sql "SELECT count(*) FROM artifacts a JOIN step_runs sr ON a.step_run_id = sr.id WHERE sr.run_id = '$RUN_ID'" | tr -d ' \n')
  if [[ "$ARTIFACT_COUNT" -gt 0 ]]; then
    pass "mcp-test — $ARTIFACT_COUNT artifact(s) created"
  else
    fail "mcp-test — no artifacts created"
  fi

  fl_sql "DELETE FROM knowledge_items WHERE summary = 'mcp-test-learning'" >/dev/null 2>&1 || true
fi

# ══════════════════════════════════════════════════════════════════
# TIER 2: Requires GITHUB_TOKEN credential
# ══════════════════════════════════════════════════════════════════

section "Tier 2: GitHub Workflows (requires GITHUB_TOKEN)"

if [[ "$HAS_GITHUB_TOKEN" != "true" ]]; then
  skip "clone-test — no GITHUB_TOKEN credential"
  skip "pr-review — no GITHUB_TOKEN credential"
  skip "triage — no GITHUB_TOKEN credential"
  skip "bug-fix — no GITHUB_TOKEN credential"
else

  run_workflow "clone-test" "clone-test" "{\"repo_url\":\"$TEST_REPO\"}" 120
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    verify_steps "clone-test" "$RUN_ID"
    CLONE_OUTPUT=$(step_output "$RUN_ID" "clone_and_verify")
    assert_contains "clone-test output has clone-ok" "${CLONE_OUTPUT:-}" "clone-ok"
  fi

  run_workflow "pr-review" "pr-review" "{\"repo_url\":\"$TEST_REPO\",\"pr_number\":52}" 300
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    verify_steps "pr-review" "$RUN_ID"
  fi

  TRIAGE_PARAMS=$(python3 -c "
import json
print(json.dumps({
  'repo_url': '$TEST_REPO',
  'issue_number': 1,
  'issue_body': 'Smoke test issue: the server returns 500 on GET /api/health when the database is down.'
}))
  ")
  run_workflow "triage" "triage" "$TRIAGE_PARAMS" 300
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    ANALYZE_STATUS=$(step_status "$RUN_ID" "analyze")
    assert_equals "triage analyze step" "complete" "$ANALYZE_STATUS"
  fi

  BUGFIX_PARAMS=$(python3 -c "
import json
print(json.dumps({
  'repo_url': '$TEST_REPO',
  'issue_body': 'Smoke test: add a comment to the top of README.md noting the last smoke test date.'
}))
  ")
  run_workflow "bug-fix" "bug-fix" "$BUGFIX_PARAMS" 300
  if [[ "$WORKFLOW_STATUS" == "COMPLETED" ]]; then
    verify_steps "bug-fix" "$RUN_ID"
  fi

fi

# ══════════════════════════════════════════════════════════════════
# TIER 3: Multi-repo / expensive workflows
# ══════════════════════════════════════════════════════════════════

section "Tier 3: Fleet Workflows (expensive, optional)"

if [[ "${SMOKE_TIER3:-}" != "1" ]]; then
  skip "fleet-research — set SMOKE_TIER3=1 to enable"
  skip "fleet-transform — set SMOKE_TIER3=1 to enable"
  skip "migration — set SMOKE_TIER3=1 to enable"
  skip "dependency-update — set SMOKE_TIER3=1 to enable"
  skip "doc-assessment — set SMOKE_TIER3=1 to enable"
  skip "audit — set SMOKE_TIER3=1 to enable"
  skip "incident-response — set SMOKE_TIER3=1 to enable"
  skip "auto-debt-slayer — requires Jira credentials"
elif [[ "$HAS_GITHUB_TOKEN" != "true" ]]; then
  skip "Tier 3 — no GITHUB_TOKEN credential"
else

  REPOS_JSON="[\"$TEST_REPO\"]"

  run_workflow "fleet-research" "fleet-research" \
    "{\"repos\":$REPOS_JSON,\"prompt\":\"List the top-level directories and summarize the project structure.\",\"max_parallel\":1}" 300

  run_workflow "fleet-transform" "fleet-transform" \
    "{\"repos\":$REPOS_JSON,\"prompt\":\"Add a comment at the top of README.md: smoke test transform.\",\"max_parallel\":1}" 300

  run_workflow "audit" "audit" \
    "{\"repos\":$REPOS_JSON,\"audit_prompt\":\"Check if the project has a LICENSE file.\"}" 300

  run_workflow "incident-response" "incident-response" \
    "{\"repo_url\":\"$TEST_REPO\",\"log_excerpt\":\"ERROR: connection refused to database\",\"incident_description\":\"Smoke test incident\"}" 300

fi

# ── Summary ───────────────────────────────────────────────────────
smoke_summary
