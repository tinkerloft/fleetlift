# MCP Agent Interface Design

**Date:** 2026-03-14
**Status:** Draft

## Problem Statement

Agents running inside FleetLift sandboxes are black boxes. They receive a prompt, run to completion, and produce output that is collected after the fact. They cannot interact with the platform during execution — no mid-run artifact creation, no requesting human input, no real-time knowledge updates, no progress reporting.

This limitation is manageable for Claude Code today, but becomes a serious constraint as we:

1. **Add more agent runtimes** (Codex, Gemini, custom agents) — each requiring bespoke output parsing and capability negotiation
2. **Support non-agentic steps** (OpenRewrite recipes, linters, static analysis) — tools that produce structured results but have no way to communicate them back to the platform
3. **Need richer human-in-the-loop** — agents should be able to ask questions mid-execution rather than only at step boundaries

## Proposal

Expose a **FleetLift MCP server** that agents and tools can connect to during execution. This provides a standardized, agent-agnostic interface for platform capabilities.

MCP (Model Context Protocol) is the right choice because:

- Claude Code supports MCP natively
- OpenAI has committed to MCP support for Codex
- Google is following with Gemini MCP support
- Non-AI tools can call MCP endpoints via simple HTTP (stdio or SSE transport)
- It replaces N agent-specific integrations with 1 server

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│ FleetLift Worker                                                │
│                                                                  │
│  ┌──────────────┐    ┌──────────────────┐    ┌────────────────┐ │
│  │ Temporal      │───▶│ ExecuteStep      │───▶│ Agent Runner   │ │
│  │ Activity      │    │ Activity         │    │ (claude/codex/ │ │
│  │               │    │                  │    │  shell)        │ │
│  └──────────────┘    └──────────────────┘    └───────┬────────┘ │
│                                                       │          │
│                       ┌──────────────────┐            │          │
│                       │ MCP Server       │◀───────────┘          │
│                       │ (per-sandbox)    │   agent connects      │
│                       │                  │   via stdio or SSE    │
│                       │ Tools:           │                       │
│                       │  - memory.*      │                       │
│                       │  - artifact.*    │                       │
│                       │  - inbox.*       │                       │
│                       │  - context.*     │                       │
│                       │  - progress.*    │                       │
│                       └───────┬──────────┘                       │
│                               │                                  │
│                    ┌──────────▼──────────┐                       │
│                    │ FleetLift Backend   │                       │
│                    │ (DB, Knowledge,     │                       │
│                    │  Inbox, Artifacts)  │                       │
│                    └────────────────────┘                        │
└──────────────────────────────────────────────────────────────────┘
```

### MCP Server Placement

The MCP server runs as a **sidecar process inside the sandbox**, started by the `ProvisionSandbox` activity before the agent launches. It communicates with the FleetLift backend over HTTPS using a per-sandbox auth token.

**Why inside the sandbox (not external):**
- Agents connect via local stdio/SSE — no cross-network latency
- Claude Code's MCP config expects local server processes
- Non-agentic tools can call it via `localhost` HTTP
- One MCP server per sandbox provides natural isolation

**Auth model:**
- `ProvisionSandbox` generates a short-lived JWT scoped to `{team_id, run_id, step_run_id}`
- Injected as `FLEETLIFT_MCP_TOKEN` env var
- MCP server validates token on every tool call
- Token expires when sandbox expires

### New Environment Variables

| Variable | Purpose |
|----------|---------|
| `FLEETLIFT_MCP_TOKEN` | Per-sandbox JWT for MCP server → backend auth |
| `FLEETLIFT_MCP_URL` | Backend URL the MCP server calls (defaults to `FLEETLIFT_API_URL`) |

## Phased Implementation

### Phase 1 — Read-Only Context Tools

**Goal:** Validate the architecture with zero write-side risk. Agents can pull context they need instead of having everything crammed into the prompt template.

**New package:** `internal/mcp/`

**Tools:**

#### `context.get_run`
Returns metadata about the current run: workflow name, parameters, step graph, current step ID.

```json
// No input required — scoped by MCP token
// Response:
{
  "run_id": "abc-123",
  "workflow": "bug-fix",
  "parameters": {"repo_url": "https://github.com/...", "issue_body": "..."},
  "current_step": "fix",
  "steps": [
    {"id": "analyze", "status": "complete"},
    {"id": "fix", "status": "running"},
    {"id": "self_review", "status": "pending"}
  ]
}
```

#### `context.get_step_output`
Returns the output of a completed prior step. Replaces `{{ .Steps.step_id.Output | toJSON }}` for agents that want to pull data on demand rather than receiving it in the prompt.

```json
// Input:
{"step_id": "analyze"}
// Response:
{"output": {"root_cause": "...", "affected_files": [...]}, "diff": "..."}
```

#### `context.get_knowledge`
Searches the knowledge base for relevant items. Replaces prompt-level knowledge enrichment with agent-driven retrieval.

```json
// Input:
{"query": "database migration patterns", "max_items": 5}
// Response:
{"items": [{"type": "pattern", "summary": "...", "details": "...", "confidence": 0.9}]}
```

**Implementation steps:**

1. Add `internal/mcp/server.go` — MCP server using `github.com/mark3labs/mcp-go` (Go MCP SDK)
2. Add `internal/mcp/tools_context.go` — read-only tool handlers
3. Add `internal/mcp/auth.go` — JWT validation scoped to sandbox
4. Add MCP binary to sandbox images (`cmd/mcp-server/main.go`)
5. Update `ProvisionSandbox` to generate MCP token and start MCP server in sandbox
6. Update `ClaudeCodeRunner` to include MCP server config in claude CLI args (`--mcp-config`)
7. Tests: unit tests for tool handlers + integration test with `testsuite`

**What this replaces:**
- Prompt-level knowledge enrichment (lines 71-85 in `execute.go`) becomes optional — agents can pull knowledge on demand
- Step output templating (`{{ .Steps.X.Output }}`) becomes optional — agents can query dynamically

**What stays the same:**
- `Runner` interface unchanged
- Workflow YAML unchanged (templates still work for non-MCP agents)
- All existing event streaming and log capture unchanged

---

### Phase 2 — Write Tools (Artifacts, Knowledge, Progress)

**Goal:** Let agents produce artifacts and knowledge during execution rather than relying on file conventions and post-step collection.

**Tools:**

#### `artifact.create`
Creates a named artifact from content or a sandbox file path. Replaces the static `outputs.artifacts` YAML declaration.

```json
// Input:
{
  "name": "security-report",
  "content": "## Findings\n...",
  "content_type": "text/markdown"
}
// Or file-based:
{
  "name": "coverage-data",
  "file_path": "/workspace/coverage.json"
}
// Response:
{"artifact_id": "art-456", "name": "security-report"}
```

**Value over current approach:** Today, artifacts must be declared in the workflow YAML before the agent runs. The agent doesn't control what becomes an artifact — the template author does. With `artifact.create`, agents can produce artifacts dynamically based on what they discover. An audit agent that finds 3 critical issues can create 3 separate finding artifacts rather than one monolithic file.

#### `memory.add_learning`
Adds a knowledge item in real-time. Replaces the `fleetlift-knowledge.json` file convention.

```json
// Input:
{
  "type": "gotcha",
  "summary": "This repo uses a custom ESLint plugin that must be installed first",
  "details": "Run `npm install eslint-plugin-custom` before linting",
  "confidence": 0.85,
  "tags": ["javascript", "linting"]
}
// Response:
{"id": "k-789", "status": "pending"}
```

#### `memory.search`
Semantic search across all approved knowledge for the team. Extends Phase 1's `context.get_knowledge` with richer query support.

```json
// Input:
{
  "query": "common failures in React migration",
  "tags": ["react", "migration"],
  "max_items": 10
}
```

#### `progress.update`
Reports step progress to the UI. Today, progress is implied by log streaming — a noisy, imprecise signal.

```json
// Input:
{"percentage": 45, "message": "Analyzing 12 of 26 controllers"}
```

**Implementation notes:**
- Write tools call the FleetLift backend API using the MCP token
- Rate limiting: max 50 artifact creates, 100 knowledge items, 10 progress updates/minute per sandbox
- Size limits: 10MB per artifact (inline), 100MB (object store), 1KB per knowledge item
- Input validation mirrors existing rules (path validation for file-based artifacts, knowledge type enum)

**Impact on existing code:**
- `CollectArtifacts` activity becomes optional (backward compat for non-MCP agents)
- `CaptureKnowledge` activity becomes optional
- `StepWorkflow` can skip post-step artifact/knowledge collection when MCP was available

---

### Phase 3 — Interactive Tools (HITL, Inbox)

**Goal:** Enable mid-execution human interaction. This is the biggest capability unlock — agents can ask humans for guidance without losing their execution context.

**Tools:**

#### `inbox.request_input`
Pauses agent execution and waits for a human response. The MCP tool call blocks until the human responds.

```json
// Input:
{
  "question": "The test suite has 3 flaky tests. Should I skip them or fix them first?",
  "options": ["Skip flaky tests", "Fix flaky tests first", "Abort the run"],
  "urgency": "normal",
  "context": "Tests: test_auth_flow, test_rate_limit, test_webhook_retry"
}
// Response (after human answers):
{"answer": "Fix flaky tests first", "responder": "jane@example.com"}
```

**Implementation detail:** This is the trickiest tool because it must bridge MCP (synchronous tool call) with Temporal (signal-based). The flow:

1. MCP server receives `inbox.request_input` call
2. MCP server calls FleetLift backend API to create inbox item + set step status to `awaiting_input`
3. MCP server long-polls (or uses SSE) waiting for a response
4. Human responds via UI → backend stores response
5. MCP server receives response → returns it to the agent
6. Agent continues execution with the answer

The existing `StepWorkflow` signal handlers (`approve`/`reject`/`steer`) would be extended with a `respond` signal that carries the human's answer back through the MCP server.

#### `inbox.notify`
Sends a notification to the team inbox without blocking. For mid-run status updates that aren't questions.

```json
// Input:
{
  "title": "Large dependency tree detected",
  "summary": "Found 847 transitive dependencies. Analysis will take longer than usual.",
  "urgency": "low"
}
// Response:
{"inbox_item_id": "inbox-321"}
```

**Value:** Today, inbox items are only created on run completion. Agents can't flag issues they discover mid-execution. A security audit agent that finds a critical vulnerability in minute 2 of a 30-minute scan can immediately notify the team.

---

## Memory Tool Assessment

The existing knowledge system (`internal/knowledge/`) provides the storage layer, but its current integration has significant limitations that an MCP memory tool would address.

### Current State

**How knowledge works today:**

1. **Capture:** Agent is instructed (via prompt suffix) to write `fleetlift-knowledge.json` before exiting. The `CaptureKnowledge` activity reads this file post-step.
2. **Enrich:** Before running an agent, `ExecuteStep` queries for approved knowledge items and prepends them to the prompt.
3. **Curation:** Humans review pending items via the API and approve/reject them.

**Problems with this approach:**

| Problem | Impact |
|---------|--------|
| **Fire-and-forget capture** | Agent writes knowledge at the very end. If it crashes or times out, insights are lost. |
| **No incremental learning** | Agent discovers insight at minute 5, but can't record it until minute 30 when it finishes. |
| **Prompt bloat** | All approved knowledge is dumped into the prompt upfront. With 50+ items, this wastes context window. |
| **No retrieval** | Agent can't search for specific knowledge mid-run. It gets everything or nothing. |
| **Cross-workflow blindness** | Knowledge is scoped to `workflow_template_id`. An insight from `bug-fix` runs isn't available to `pr-review` runs for the same repo. |
| **Agent-specific format** | The `fleetlift-knowledge.json` convention only works for AI agents that understand the instruction. Shell steps and non-agentic tools can't contribute. |

### What MCP Memory Tools Enable

#### `memory.add_learning` (Phase 2)

Real-time knowledge capture during execution:

- Agent records insights as it discovers them — no lost knowledge on crash/timeout
- Non-agentic steps (OpenRewrite, linters) can record patterns via simple HTTP call
- Each learning is immediately persisted and visible to the curation queue

#### `memory.search` (Phase 2)

On-demand, contextual knowledge retrieval:

- Agent queries for relevant knowledge when it needs it, not all upfront
- Reduces prompt bloat — only fetch knowledge related to the current sub-problem
- Supports tag-based and free-text search
- Can search cross-workflow (team-scoped) not just current workflow template

#### `memory.get_repo_context` (Future)

Repository-specific accumulated context:

- "What do we know about repo X from all previous runs?"
- Aggregates knowledge items tagged with a repo URL
- Useful for agents working on unfamiliar repos — learn from prior agents' experience

### Value Assessment

**High value, and the incremental cost is low.** The storage layer (`knowledge.Store`) already exists with full CRUD. The MCP tools are thin wrappers:

- `memory.add_learning` → calls `knowledge.Store.Save()` with team/step scoping from the MCP token
- `memory.search` → calls `knowledge.Store.ListApprovedByWorkflow()` (extend with free-text search)
- No new database tables, no schema changes

**The real unlock is behavioral:** When knowledge capture is a tool call instead of a file convention, agents use it naturally as part of their workflow. Claude Code will call `memory.add_learning` when it discovers something noteworthy — just like it calls `Write` to create a file. It doesn't need a special prompt instruction telling it to "write a JSON file before exiting."

**Concrete scenarios where memory tools add value:**

1. **Fleet transforms** (`fleet-transform.yaml`): Agent transforms repo 1, learns that the codebase uses a custom import alias. Records it. Agent transforming repo 2 (parallel, different sandbox) searches memory and finds the pattern — applies it immediately instead of discovering it from scratch.

2. **Recurring bug fixes**: Agent fixes a bug in a microservice, records a gotcha about the testing setup. Next month, a different agent fixing a different bug in the same service searches memory and avoids the same pitfall.

3. **Non-agentic enrichment**: An OpenRewrite recipe step runs a migration and discovers 15 files couldn't be auto-migrated. A wrapper script calls `memory.add_learning` with the list. The next (agentic) step searches memory and handles the exceptions.

4. **Cross-workflow learning**: A security audit discovers that repo X has a custom auth middleware. A later PR review workflow for the same repo searches memory and knows to check auth changes carefully.

**Recommendation:** Memory tools should be in Phase 2 alongside artifacts and progress. They reuse existing infrastructure, the implementation is straightforward, and they solve a real problem with the current file-based knowledge capture approach.

### Future: Semantic Memory

Beyond the structured knowledge items, a future phase could add:

- **Embeddings-based search** over knowledge items for fuzzy matching
- **Auto-deduplication** — detect when an agent is adding knowledge that already exists
- **Decay/refresh** — knowledge items that haven't been useful in N runs get demoted
- **Memory consolidation** — periodically summarize many small items into fewer high-quality ones

This is out of scope for the initial implementation but worth designing the storage layer to accommodate (e.g., add an `embedding` vector column to `knowledge_items`).

---

## Impact on Non-Agentic Steps

Non-agentic steps (shell commands, OpenRewrite, linters) benefit from MCP without being MCP-native. The MCP server in the sandbox exposes an HTTP endpoint alongside the stdio transport. A shell step can interact via `curl`:

```bash
# OpenRewrite recipe wrapper
./gradlew rewriteRun --recipe=org.openrewrite.java.Spring6Migration

# Report results to FleetLift
curl -X POST http://localhost:$MCP_PORT/artifact/create \
  -H "Authorization: Bearer $FLEETLIFT_MCP_TOKEN" \
  -d '{"name": "migration-results", "file_path": "/workspace/rewrite-results.json"}'

# Record a learning
curl -X POST http://localhost:$MCP_PORT/memory/add_learning \
  -H "Authorization: Bearer $FLEETLIFT_MCP_TOKEN" \
  -d '{"type": "pattern", "summary": "Spring 6 migration requires Java 17+", "confidence": 1.0}'
```

This means any tool that can run in a sandbox gets first-class platform integration without being an AI agent.

## Impact on Multi-Agent Support

Today, adding a new agent runtime requires:

1. A new `Runner` implementation (`internal/agent/`)
2. A custom output event parser (see `parseClaudeEvent` — 120 lines of Claude-specific parsing)
3. Understanding each agent's structured output format
4. Agent-specific prompt suffixes (e.g., the `fleetlift-knowledge.json` instruction)

With MCP, a new agent runtime requires only:

1. A new `Runner` implementation (launch command + basic event streaming)
2. MCP server config injected into the agent's environment

The agent discovers capabilities through MCP tool listing. Knowledge capture, artifact creation, and HITL work identically regardless of whether the agent is Claude, Codex, or Gemini. The `parseClaudeEvent` style parsing is still needed for log streaming, but the structured platform interactions move to MCP.

Note: `provision.go` already has `agentImage()` with a `codex` case (line 79), so multi-agent support is anticipated at the infrastructure level. MCP closes the capability gap.

## Backward Compatibility

All phases maintain full backward compatibility:

- Workflow YAML `outputs.artifacts` still works (collected post-step as today)
- `knowledge.capture: true` still works (file-based capture as today)
- Prompt-level knowledge enrichment still works
- Agents that don't connect to MCP run exactly as they do now
- MCP is additive — no existing behavior changes

The MCP server is started in the sandbox regardless, but if no agent connects to it, it sits idle and costs nothing.

## Open Questions

1. **MCP transport:** Should we use stdio (natural for Claude Code) or SSE (easier for non-agentic `curl` usage)? Likely both — stdio for AI agents, HTTP/SSE for shell scripts.

2. **Fan-out knowledge sharing:** In parallel fan-out steps, should one agent's `memory.add_learning` be immediately searchable by sibling agents? This requires the MCP server to write through to the backend synchronously, which it would do anyway.

3. **`request_input` timeout:** How long should the MCP tool call block waiting for a human response? Should there be a configurable timeout with a default (e.g., 4 hours)?

4. **MCP server lifecycle:** Should the MCP server be a separate binary in the sandbox image, or bundled into the agent runner? Separate binary is cleaner but requires maintaining another build artifact.

5. **Rate limiting implementation:** Per-sandbox in-memory counters in the MCP server, or centralized in the backend? In-memory is simpler but resets if MCP server restarts.
