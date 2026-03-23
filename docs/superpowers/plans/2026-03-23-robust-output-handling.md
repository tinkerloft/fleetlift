# Robust Claude Code Output Handling - Implementation Plan

> For agentic workers: use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Keep checkbox state updated as work progresses.

**Goal:** Define a stable Fleetlift-owned input/output contract for Claude Code agents running in a sandbox, and use that contract to stream the same user-visible transcript a person would see when using the sandboxed agent directly, without drowning the user in low-value noise.

This plan has three primary outcomes:

1. Replace the hand-rolled output schema validator with a real JSON Schema library.
2. Replace direct dependence on the fragile `claude --output-format stream-json` CLI surface with a Node.js bridge that forms the stable contract boundary between Fleetlift and Claude Code.
3. Emit a stable normalized transcript stream from that bridge: assistant narration, tool calls, tool results, progress/status, HITL prompts, completion, and errors.

**Non-goal:** A per-step `show_thinking` toggle is not part of the initial implementation. If the SDK exposes user-visible thinking blocks as part of the normal transcript, capture them as transcript events. Do not build support for hidden/raw model reasoning.

**Stable contract boundary:**

Fleetlift should not depend on the exact shape of Claude Code's raw CLI output. The bridge becomes the compatibility layer for both directions:

- **Inputs to agent:** a versioned bridge request document owned by Fleetlift, containing prompt file path, working directory, turn limits, MCP/plugin configuration, and other execution options Fleetlift chooses to support.
- **Outputs from agent:** normalized transcript events owned by Fleetlift, not pass-through upstream SDK/CLI payloads.

This makes upstream Claude Code changes easier to absorb in one place and gives Fleetlift a contract it can test explicitly.

**Architecture:**

- Part A swaps `validateOutputSchema` in `internal/activity/execute.go` to `github.com/santhosh-tekuri/jsonschema/v5`.
- Part B adds `docker/bridge.js` to the sandbox image. The bridge uses `@anthropic-ai/claude-code` programmatically and is the only Fleetlift-to-Claude-Code contract boundary. It accepts stable Fleetlift-owned inputs and emits newline-delimited JSON transcript events with a stable schema.
- Part C updates `internal/agent/claudecode.go` and related parsing/tests to consume the bridge format, preserve high-fidelity output, and avoid truncation-related data loss.

**Tech Stack:** Go 1.25, `github.com/santhosh-tekuri/jsonschema/v5`, Node.js 20, `@anthropic-ai/claude-code`.

## Success Criteria

- [x] Users see assistant text, tool calls, tool outputs, status updates, completion, and errors in near real time.
- [x] Large tool output is preserved via chunking; no arbitrary 8 KB truncation in the bridge.
- [x] The event stream remains parseable and stable even if upstream CLI output changes.
- [x] The bridge contract for both inputs and outputs is documented and covered by tests.
- [x] HITL / `needs_input` behavior still works.
- [x] Output schema validation behavior remains compatible with existing workflow expectations.
- [x] `make lint`, `go test ./...`, and `go build ./...` all pass.

---

## Part 0 - Discovery Gate

Do this before writing the bridge contract. The current plan guessed too much about the SDK surface.

### Task 0: Verify the programmatic SDK shape and define the contract

**Files:** read-only exploration

- [x] **Step 1: Inspect package format and exports**

```bash
docker run --rm claude-code-sandbox:latest \
  node -e "import('@anthropic-ai/claude-code').then(m => console.log(JSON.stringify(Object.keys(m))))"
```

If that fails, try the CommonJS form separately and record which import style works.

- [x] **Step 2: Inspect real event payloads from `query()`**

```bash
docker run --rm -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY claude-code-sandbox:latest \
  node -e "import('@anthropic-ai/claude-code').then(async ({ query }) => { for await (const msg of query({ prompt: 'say hi in one word', options: { maxTurns: 1 } })) { console.log(JSON.stringify({ type: msg.type, keys: Object.keys(msg), sample: msg })) } })"
```

Capture and document:

- actual event types
- assistant text block shape
- tool call block shape
- tool result block shape
- completion payload fields
- any visible thinking/status/progress blocks
- any `needs_input` / HITL-like events
- supported options for permissions, plugin dirs, cwd, and turn limits

- [x] **Step 3: Decide the stable Fleetlift-to-bridge input contract**

Document the bridge invocation contract explicitly. Recommend a single request JSON file rather than positional argv so the contract is self-describing and versionable.

Recommended shape:

```json
{
  "version": 1,
  "prompt_file": "/tmp/fleetlift-prompt-abc123.txt",
  "work_dir": "/workspace/repo",
  "max_turns": 5,
  "mcp": {
    "enabled": true,
    "config_path": "/workspace/.mcp.json"
  },
  "plugin_dirs": ["/agent/plugins/foo"],
  "env": {
    "FOO": "bar"
  }
}
```

At minimum, define:

- exact invocation shape, e.g. `node /agent/bridge.js /tmp/fleetlift-request-abc123.json`
- required vs optional request fields
- how Fleetlift versions the request format (`version` required)
- which defaults Fleetlift owns vs which defaults the bridge owns
- what fields are guaranteed stable for unit tests

Prefer a contract that is simple, explicit, and easy to validate in unit tests.

- [x] **Step 4: Decide the bridge output contract from observed behavior**

Record the normalized event types Fleetlift will support:

- `assistant_text`
- `tool_call`
- `tool_result`
- `status`
- `needs_input`
- `complete`
- `error`

If the SDK exposes user-visible thinking blocks, include either `thinking` or fold them into `assistant_text`. Do not add a hidden-reasoning feature.

- [x] **Step 5: Confirm large-output strategy**

The bridge must not emit one giant unbounded line for huge tool results. Decide and document chunking fields now. Recommended shape:

```json
{"type":"tool_result","call_id":"...","stream":"stdout","chunk_index":1,"chunk_total":3,"content":"..."}
```

Chunking is required for robustness and to avoid scanner/line-size failures.

---

## Part A - JSON Schema Validation

### Task 1: Replace hand-rolled validation with `jsonschema/v5`

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/activity/execute.go`
- Modify: `internal/activity/execute_test.go`

- [x] **Step 1: Add the dependency**

```bash
go get github.com/santhosh-tekuri/jsonschema/v5@latest
```

- [x] **Step 2: Add regression tests before swapping the implementation**

Cover at least:

- required fields still enforced
- string / boolean / number / array / object types
- nested object accepted when schema says `object`
- undeclared fields behavior is explicit and tested
- empty schema still passes

Do not overfit tests to exact error strings from the library; assert semantic failures.

- [x] **Step 3: Implement a schema builder with explicit semantics**

In `internal/activity/execute.go`:

- add a helper that converts the current flat workflow schema map into a JSON Schema document
- explicitly set `required`
- explicitly decide `additionalProperties` behavior to match the current validator
- keep the supported type vocabulary unchanged for this migration

- [x] **Step 4: Replace `validateOutputSchema`**

Use `jsonschema/v5` for compile + validate. If compilation can happen repeatedly for the same schema, prefer reusing compiled schema objects instead of recompiling each time.

- [x] **Step 5: Run targeted tests**

```bash
go test ./internal/activity/... -run 'TestValidateOutputSchema|TestBuildJSONSchema' -v
```

- [x] **Step 6: Run required verification**

```bash
make lint
go test ./...
go build ./...
```

---

## Part B - Transcript Bridge

### Task 2: Add a stable transcript bridge to the sandbox image

**Files:**

- Create: `docker/bridge.js`
- Modify: `docker/Dockerfile.sandbox`

### Bridge requirements

The bridge is the stable I/O contract boundary between Fleetlift and Claude Code. It should accept a single Fleetlift-owned request JSON file and emit one normalized JSON event per line. Recommended event shapes:

Recommended invocation:

```bash
node /agent/bridge.js /tmp/fleetlift-request-abc123.json
```

Recommended request document:

```json
{
  "version": 1,
  "prompt_file": "/tmp/fleetlift-prompt-abc123.txt",
  "work_dir": "/workspace/repo",
  "max_turns": 5,
  "mcp": {
    "enabled": true,
    "config_path": "/workspace/.mcp.json"
  },
  "plugin_dirs": ["/agent/plugins/foo"]
}
```

Recommended event shapes:

```json
{"type":"assistant_text","content":"Working on it..."}
{"type":"tool_call","name":"Bash","command":"git status"}
{"type":"tool_result","call_id":"toolu_123","stream":"stdout","chunk_index":1,"chunk_total":2,"content":"..."}
{"type":"status","content":"Running tests"}
{"type":"needs_input","content":"Approve deployment?"}
{"type":"complete","result":"Done","cost_usd":0.04,"is_error":false}
{"type":"error","content":"API rate limit"}
```

Rules:

- Fleetlift talks to the bridge contract, not directly to raw Claude Code CLI output
- capture what a direct user would see
- preserve full content through chunking rather than truncation
- filter only low-value internal noise (`system` frames that are never user-visible)
- keep event names simple and Fleetlift-owned so upstream SDK changes do not leak through directly

- [x] **Step 1: Write parser tests for the normalized event contract**

In `internal/agent/claudecode_test.go`, add tests for:

- `assistant_text`
- `tool_call`
- `tool_result` stdout
- `tool_result` stderr
- `status`
- `needs_input`
- `complete`
- `error`
- chunked `tool_result` events

These should initially fail until parser support is added.

Also add runner-side tests for the input contract itself:

- bridge is invoked with a request JSON file path
- request JSON includes `version`
- request JSON includes `prompt_file`, `work_dir`, and `max_turns`
- plugin/MCP fields are serialized consistently

- [x] **Step 2: Implement `docker/bridge.js`**

Bridge behavior:

- accept exactly one argument: path to the Fleetlift request JSON file
- parse and validate the request document, including `version`
- read prompt from `prompt_file`, not argv
- call the SDK programmatically using the import style verified in Part 0
- map SDK events into the normalized Fleetlift transcript schema
- emit assistant narration as `assistant_text`
- emit tool invocations as `tool_call`
- emit tool outputs as `tool_result`
- emit visible progress updates as `status`
- emit HITL prompts as `needs_input` if available
- emit `complete` / `error`
- chunk large tool results; do not truncate them

Recommended implementation detail: cap each emitted `content` chunk to something comfortably under the scanner limit, then still increase the Go scanner buffer as defense in depth.

- [x] **Step 3: Copy the bridge into the sandbox image**

Update `docker/Dockerfile.sandbox` to include the script and mark it executable.

- [x] **Step 4: Rebuild the sandbox image and smoke-test the bridge**

```bash
docker build -f docker/Dockerfile.sandbox -t claude-code-sandbox:latest .
docker run --rm claude-code-sandbox:latest node /agent/bridge.js --help 2>&1 || true
docker run --rm claude-code-sandbox:latest test -f /agent/bridge.js
```

If practical, also run a one-prompt smoke test with `ANTHROPIC_API_KEY` set and inspect the emitted event types.

---

## Part C - Runner And Parser Integration

### Task 3: Update `ClaudeCodeRunner` to use the bridge

**Files:**

- Modify: `internal/agent/claudecode.go`
- Modify: `internal/agent/runner.go` if needed
- Modify: `internal/agent/claudecode_test.go`

- [x] **Step 1: Add failing tests for bridge invocation**

Cover at least:

- runner invokes `node /agent/bridge.js`, not `claude -p --output-format stream-json`
- runner writes a unique prompt temp file
- runner writes a unique request JSON file
- request JSON path is passed to the bridge as the only required arg
- request JSON contains `version`, `prompt_file`, `work_dir`, and `max_turns`
- eval/plugin dirs are preserved if the SDK supports them

- [x] **Step 2: Update `ClaudeCodeRunner.Run`**

Implementation requirements:

- write the prompt to a unique temp file inside the sandbox
- write a unique request JSON file inside the sandbox
- configure MCP sidecar env/setup as today
- invoke the bridge with the request JSON path instead of many positional args
- keep shell quoting for every user-controlled value
- continue streaming events through `ExecStream`

- [x] **Step 3: Update `parseClaudeEvent` for the normalized transcript contract**

Map normalized events to Fleetlift `Event` values:

- `assistant_text` -> `stdout`
- `tool_call` -> `stdout` with readable formatting
- `tool_result` -> `stdout` or `stderr` based on stream/error metadata
- `status` -> `system` or `stdout` based on how existing consumers use status lines
- `needs_input` -> `needs_input`
- `complete` -> `complete`
- `error` -> `error`

Keep backward compatibility with the legacy parser only if needed for migration. If the bridge rollout is atomic, prefer deleting dead parsing branches rather than carrying both formats forever.

- [x] **Step 4: Increase scanner buffer size where output is read**

Any `bufio.Scanner` reading bridge output must call `scanner.Buffer(..., 4*1024*1024)` or larger. This is required by repo policy and is especially important once tool results are chunked rather than truncated.

- [x] **Step 5: Run agent tests**

```bash
go test ./internal/agent/... -v
```

---

## Part D - Transcript Fidelity And Noise Control

### Task 4: Preserve full transcript while keeping the default stream readable

This task is intentionally about capture semantics, not a new workflow flag.

**Files:**

- Modify existing event/log handling only if needed to preserve richer transcript data
- Add tests in the relevant package(s)

- [x] **Step 1: Define what is always captured**

Always capture:

- assistant narration
- tool calls
- tool results
- user-visible status/progress
- completion/errors
- HITL prompts

If the SDK exposes user-visible thinking blocks, capture them too. If it does not, do nothing special.

- [x] **Step 2: Keep compact presentation separate from capture**

Do not discard transcript data just to keep logs shorter. If the consumer wants a compact view, that should be a presentation concern. The backend event stream should preserve the full transcript as emitted by the bridge.

- [x] **Step 3: Add tests for large-output handling**

Cover at least:

- multi-chunk tool result is preserved in order
- stderr chunks remain stderr
- empty/unknown SDK frames do not crash parsing
- `needs_input` still propagates correctly

---

## Deferred / Out Of Scope For This Plan

- [ ] Per-step YAML `show_thinking` toggle
- [ ] Hidden/raw chain-of-thought exposure
- [ ] UI-specific compact/full transcript controls, unless required to keep backend changes usable

If later needed, add a separate follow-up plan for transcript presentation controls rather than baking them into workflow execution config.

---

## Final Verification

- [x] `make lint`
- [x] `go test ./...`
- [x] `go build ./...`
- [x] sandbox image builds successfully
- [x] bridge smoke test shows normalized transcript events
- [x] no arbitrary bridge-side truncation remains

---

## Notes / Open Questions

1. What import style does `@anthropic-ai/claude-code` actually require in the sandbox image: ESM, CommonJS, or both?
2. Does the SDK expose a first-class event for HITL / `needs_input`, or do we need to synthesize one from another visible frame?
3. Does the SDK expose plugin-dir / MCP-equivalent configuration programmatically, or is that currently CLI-only?
4. Are visible progress/thinking blocks separate event types, or are they just assistant text blocks in practice?
5. Which existing reader path owns the scanner buffer so we can guarantee large bridge output is safe end to end?
