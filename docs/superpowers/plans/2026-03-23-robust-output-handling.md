# Robust Claude Code Output Handling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hand-rolled output schema validation with a proper JSON Schema library; replace the fragile `--output-format stream-json` CLI parser with a stable Node.js bridge script that emits rich human-readable output including tool results; and add a per-step `show_thinking` flag to optionally surface Claude's reasoning.

**Architecture:** Three independent improvements. (1) Swap `validateOutputSchema` in `internal/activity/execute.go` for `github.com/santhosh-tekuri/jsonschema/v5`. (2) Add `docker/bridge.js` to the sandbox base image — invokes Claude Code programmatically via `@anthropic-ai/claude-code`, emitting text, tool calls, tool results, and optionally thinking blocks in a stable newline-delimited JSON format. (3) Thread a `show_thinking bool` flag from `ExecutionDef` → `ResolvedStepOpts` → `RunOpts` → bridge arg so individual steps can opt into seeing Claude's reasoning.

**Tech Stack:** Go 1.25, `github.com/santhosh-tekuri/jsonschema/v5`, Node.js 20 (already in sandbox), `@anthropic-ai/claude-code` (already installed globally in sandbox).

---

## Part A — JSON Schema Validation

### Task 1: Add dependency and write the schema builder

**Files:**
- Modify: `go.mod` / `go.sum` (dependency)
- Modify: `internal/activity/execute.go` — add `buildJSONSchema()`, replace `validateOutputSchema`

- [ ] **Step 1: Add the jsonschema library**

```bash
cd /Users/andrew/dev/projects/fleetlift
go get github.com/santhosh-tekuri/jsonschema/v5@latest
```

Expected: `go.mod` and `go.sum` updated, `go: added github.com/santhosh-tekuri/jsonschema/v5 ...`

- [ ] **Step 2: Write regression tests for the new validator**

These tests verify the new library preserves the old validator's behavior. They are written before the swap so that if the new implementation regresses, we catch it immediately. Add after the existing `TestValidateOutputSchema_*` tests in `internal/activity/execute_test.go`:

```go
// Regression tests — confirm new jsonschema/v5 implementation preserves
// the old validateOutputSchema behavior for types that existed before.
func TestValidateOutputSchema_BooleanType(t *testing.T) {
    schema := map[string]any{"flag": "boolean"}
    violations := validateOutputSchema(map[string]any{"flag": true}, schema)
    assert.Empty(t, violations)
    violations = validateOutputSchema(map[string]any{"flag": "yes"}, schema)
    require.Len(t, violations, 1)
    assert.Contains(t, violations[0], "flag")
}

func TestValidateOutputSchema_NumberType(t *testing.T) {
    schema := map[string]any{"count": "number"}
    violations := validateOutputSchema(map[string]any{"count": 42.0}, schema)
    assert.Empty(t, violations)
    violations = validateOutputSchema(map[string]any{"count": "not-a-number"}, schema)
    require.Len(t, violations, 1)
    assert.Contains(t, violations[0], "count")
}

func TestValidateOutputSchema_NestedObject(t *testing.T) {
    schema := map[string]any{"details": "object"}
    violations := validateOutputSchema(
        map[string]any{"details": map[string]any{"file": "main.go"}},
        schema,
    )
    assert.Empty(t, violations)
}
```

- [ ] **Step 3: Run regression tests against current code to confirm they pass**

```bash
go test ./internal/activity/... -run "TestValidateOutputSchema|TestBuildJSONSchema" -v
```

Expected: all pass. These are regression tests — they must pass both before and after the swap.

- [ ] **Step 4: Replace `validateOutputSchema` with jsonschema library**

In `internal/activity/execute.go`, replace the `validateOutputSchema` function (lines ~513–546) and add a `buildJSONSchema` helper:

```go
import (
    // add to existing imports:
    "encoding/json"
    jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
    "strings"
)

// buildJSONSchema converts the workflow's flat schema map ({"field": "type"})
// to a JSON Schema document understood by the jsonschema library.
// All declared fields are treated as required.
func buildJSONSchema(schema map[string]any) string {
    props := make(map[string]any, len(schema))
    required := make([]string, 0, len(schema))
    for field, typVal := range schema {
        typ, _ := typVal.(string)
        switch typ {
        case "array":
            props[field] = map[string]any{"type": "array"}
        case "boolean":
            props[field] = map[string]any{"type": "boolean"}
        case "number":
            props[field] = map[string]any{"type": "number"}
        case "object":
            props[field] = map[string]any{"type": "object"}
        default:
            props[field] = map[string]any{"type": "string"}
        }
        required = append(required, field)
    }
    sort.Strings(required)
    doc := map[string]any{
        "$schema":    "https://json-schema.org/draft/2020-12/schema",
        "type":       "object",
        "properties": props,
        "required":   required,
    }
    b, _ := json.Marshal(doc)
    return string(b)
}

// validateOutputSchema validates output against the declared schema using the
// jsonschema library. Returns sorted violation messages.
func validateOutputSchema(output map[string]any, schema map[string]any) []string {
    if len(schema) == 0 {
        return nil
    }
    schemaDoc := buildJSONSchema(schema)
    compiler := jsonschema.NewCompiler()
    if err := compiler.AddResource("schema.json", strings.NewReader(schemaDoc)); err != nil {
        return []string{fmt.Sprintf("internal: build schema: %v", err)}
    }
    compiled, err := compiler.Compile("schema.json")
    if err != nil {
        return []string{fmt.Sprintf("internal: compile schema: %v", err)}
    }
    if err := compiled.Validate(output); err != nil {
        // Walk the Causes tree to collect leaf-level violation messages.
        // Each leaf describes one constraint failure (missing field, wrong type, etc.).
        var ve *jsonschema.ValidationError
        if errors.As(err, &ve) {
            msgs := collectViolations(ve)
            sort.Strings(msgs)
            if len(msgs) > 0 {
                return msgs
            }
        }
        return []string{err.Error()}
    }
    return nil
}

// collectViolations walks a ValidationError tree and returns leaf messages.
func collectViolations(ve *jsonschema.ValidationError) []string {
    if len(ve.Causes) == 0 {
        return []string{ve.Message}
    }
    var msgs []string
    for _, c := range ve.Causes {
        msgs = append(msgs, collectViolations(c)...)
    }
    return msgs
}
```

Also add `"errors"` to the import block.

Delete the old `validateOutputSchema` function entirely (lines ~513–546 in current file).

- [ ] **Step 5: Run all tests**

```bash
go test ./internal/activity/... -v 2>&1 | tail -30
```

Expected: all pass including `TestValidateOutputSchema_*` and `TestBuildJSONSchema_*`.

- [ ] **Step 6: Lint check**

```bash
make lint
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/activity/execute.go internal/activity/execute_test.go
git commit -m "feat: replace hand-rolled schema validator with jsonschema/v5 library"
```

---

## Part B — Node.js Bridge Script

### Task 2: Discover the @anthropic-ai/claude-code programmatic API

The `@anthropic-ai/claude-code` package is already installed globally in the sandbox base image (`Dockerfile.sandbox` line 23). Before writing the bridge, verify its API shape.

**Files:**
- Read-only exploration

- [ ] **Step 1: Inspect the installed module's exports**

```bash
docker run --rm claude-code-sandbox:latest \
  node -e "const cc = require('@anthropic-ai/claude-code'); console.log(JSON.stringify(Object.keys(cc)))"
```

Expected: a list like `["query","ClaudeCode","...]` — confirms `query` is exported.

- [ ] **Step 2: Inspect what query() emits**

```bash
docker run --rm -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY claude-code-sandbox:latest \
  node -e "
const { query } = require('@anthropic-ai/claude-code');
(async () => {
  for await (const msg of query({ prompt: 'say hi in one word', options: { maxTurns: 1 } })) {
    console.log(JSON.stringify({ type: msg.type, keys: Object.keys(msg) }));
  }
})();"
```

Expected: lines showing event types like `{"type":"system",...}`, `{"type":"assistant",...}`, `{"type":"result",...}`.

Note the exact field names for `result`-type events (`result`, `total_cost_usd`, `is_error`) — these inform the bridge output format.

---

### Task 3: Write the bridge script

**Files:**
- Create: `docker/bridge.js`
- Modify: `docker/Dockerfile.sandbox` — copy bridge into image

- [ ] **Step 1: Write tests for the bridge event format**

The current `parseClaudeEvent` falls through to `default` for unknown types and extracts `raw["content"]`. This means `BridgeText` and `BridgeError` will pass today (they have a `content` field), but `BridgeTool` and `BridgeComplete` will fail (no `content` field, returns empty Event). Add all four tests so the full contract is asserted:

In `internal/agent/claudecode_test.go`, add:

```go
// Bridge format tests — events emitted by docker/bridge.js.
// BridgeText and BridgeError pass today via the default fallback; BridgeTool
// and BridgeComplete fail because the bridge-specific types ("tool", "complete")
// need explicit cases added in Step 4.
func TestParseClaudeEvent_BridgeText(t *testing.T) {
    line := `{"type":"text","content":"Working on it..."}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "stdout", ev.Type)
    assert.Equal(t, "Working on it...", ev.Content)
}

func TestParseClaudeEvent_BridgeTool(t *testing.T) {
    line := `{"type":"tool","name":"Bash","command":"git status"}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "stdout", ev.Type)
    assert.Contains(t, ev.Content, "[tool] Bash: git status")
}

func TestParseClaudeEvent_BridgeComplete(t *testing.T) {
    line := `{"type":"complete","result":"All done","cost_usd":0.04,"is_error":false}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "complete", ev.Type)
    assert.Equal(t, "complete", ev.Output["type"])
    assert.Equal(t, 0.04, ev.Output["cost_usd"])
}

func TestParseClaudeEvent_BridgeError(t *testing.T) {
    line := `{"type":"error","content":"API rate limit"}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "error", ev.Type)
    assert.Equal(t, "API rate limit", ev.Content)
}
```

Also add tool result tests:

```go
func TestParseClaudeEvent_BridgeToolResult(t *testing.T) {
    line := `{"type":"tool_result","is_error":false,"content":"ok  github.com/foo/bar  1.2s"}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "stdout", ev.Type)
    assert.Contains(t, ev.Content, "github.com/foo/bar")
}

func TestParseClaudeEvent_BridgeToolResultError(t *testing.T) {
    line := `{"type":"tool_result","is_error":true,"content":"command not found: acli"}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "stderr", ev.Type)
    assert.Equal(t, "command not found: acli", ev.Content)
}
```

Run to confirm partial failure:
```bash
go test ./internal/agent/... -run TestParseClaudeEvent_Bridge -v
```

Expected: `BridgeText` and `BridgeError` PASS, `BridgeTool`, `BridgeComplete`, `BridgeToolResult`, `BridgeToolResultError` FAIL.

- [ ] **Step 2: Create docker/bridge.js**

The bridge reads the prompt from a temp file (to avoid shell-escaping limits), invokes `@anthropic-ai/claude-code` programmatically, and emits one JSON line per event:

```js
#!/usr/bin/env node
// docker/bridge.js
// Invokes Claude Code via the @anthropic-ai/claude-code programmatic API
// and emits a stable, simplified newline-delimited JSON event stream.
//
// Usage: node /agent/bridge.js <promptFile> <workDir> <maxTurns>
// - promptFile: path to a file containing the prompt text
// - workDir:    working directory for claude (default: /workspace)
// - maxTurns:   maximum agent turns (default: 100)
//
// Output events (one JSON object per line):
//   {"type":"text","content":"..."}          — assistant text
//   {"type":"tool","name":"...","command":"..."} — tool invocation
//   {"type":"complete","result":"...","cost_usd":0.0,"is_error":false}
//   {"type":"error","content":"..."}         — fatal error

'use strict';

const fs = require('fs');
const path = require('path');
const { query } = require('@anthropic-ai/claude-code');

const [, , promptFile, workDir = '/workspace', maxTurnsStr = '100', ...pluginDirs] = process.argv;

if (!promptFile) {
  process.stderr.write('usage: node bridge.js <promptFile> [workDir] [maxTurns] [pluginDir...]\n');
  process.exit(1);
}

const prompt = fs.readFileSync(promptFile, 'utf8');
const maxTurns = parseInt(maxTurnsStr, 10) || 100;

function emit(obj) {
  process.stdout.write(JSON.stringify(obj) + '\n');
}

(async () => {
  try {
    for await (const msg of query({
      prompt,
      options: {
        cwd: path.resolve(workDir),
        maxTurns,
        dangerouslySkipPermissions: true,
        // pluginDirs forwarded from EvalPluginDirs in RunOpts.
        // Verify exact option name against @anthropic-ai/claude-code API in Task 2.
        ...(pluginDirs.length > 0 ? { pluginDirs } : {}),
      },
    })) {
      switch (msg.type) {
        case 'assistant': {
          // Extract text and tool_use blocks from assistant messages.
          const blocks = msg.message?.content ?? [];
          for (const block of blocks) {
            if (block.type === 'text' && block.text) {
              emit({ type: 'text', content: block.text });
            } else if (block.type === 'tool_use') {
              emit({
                type: 'tool',
                name: block.name,
                command: block.input?.command ?? block.input?.description ?? '',
              });
            }
          }
          break;
        }
        case 'result': {
          emit({
            type: 'complete',
            result: msg.result ?? '',
            cost_usd: msg.total_cost_usd ?? msg.cost_usd ?? 0,
            is_error: msg.is_error ?? false,
          });
          break;
        }
        case 'user': {
          // Tool results — what Claude got back from each tool call.
          // This is the rich output a human would see: bash command output,
          // file contents, diffs, etc. Truncate at 8KB to keep logs manageable.
          const blocks = msg.message?.content ?? [];
          for (const block of blocks) {
            if (block.type !== 'tool_result') continue;
            const raw = Array.isArray(block.content)
              ? block.content.filter(b => b.type === 'text').map(b => b.text).join('\n')
              : (block.content ?? '');
            if (!raw) continue;
            const content = raw.length > 8192
              ? raw.slice(0, 8192) + '\n…[truncated]'
              : raw;
            emit({ type: 'tool_result', is_error: block.is_error ?? false, content });
          }
          break;
        }
        case 'system':
          // Filtered — not useful downstream.
          break;
        default:
          // Forward unknown types in case the SDK adds new ones.
          emit({ type: msg.type, content: JSON.stringify(msg) });
      }
    }
  } catch (err) {
    emit({ type: 'error', content: err.message });
    process.exit(1);
  }
})();
```

- [ ] **Step 3: Add bridge to Dockerfile.sandbox**

In `docker/Dockerfile.sandbox`, add after the `npm install -g @anthropic-ai/claude-code` line (before the `useradd` block):

```dockerfile
# Install bridge script for programmatic Claude Code invocation
COPY docker/bridge.js /agent/bridge.js
RUN chmod +x /agent/bridge.js
```

Note: this requires the Docker build context to include the `docker/` directory, which it already does for `ads-fix-rules.md`.

- [ ] **Step 4: Update parseClaudeEvent to handle bridge format**

The bridge emits a clean, single-level JSON format. Update `parseClaudeEvent` in `internal/agent/claudecode.go` to handle both the bridge format and the legacy CLI format (for backward-compatibility during migration):

```go
// parseClaudeEvent parses a single line of output from docker/bridge.js
// (preferred) or the legacy claude --output-format stream-json CLI format.
//
// Bridge format (single-level, stable):
//   {"type":"text","content":"..."}
//   {"type":"tool","name":"Bash","command":"..."}
//   {"type":"complete","result":"...","cost_usd":0.04,"is_error":false}
//   {"type":"error","content":"..."}
func parseClaudeEvent(line string) Event {
    var raw map[string]any
    if err := json.Unmarshal([]byte(line), &raw); err != nil {
        return Event{Type: "stdout", Content: line}
    }

    typ, _ := raw["type"].(string)

    // Bridge format — simple top-level type field.
    switch typ {
    case "text":
        content, _ := raw["content"].(string)
        return Event{Type: "stdout", Content: content}
    case "tool":
        name, _ := raw["name"].(string)
        cmd, _ := raw["command"].(string)
        if len(cmd) > 120 {
            cmd = cmd[:120] + "…"
        }
        return Event{Type: "stdout", Content: fmt.Sprintf("[tool] %s: %s", name, cmd)}
    case "complete":
        return Event{Type: "complete", Output: raw}
    case "tool_result":
        content, _ := raw["content"].(string)
        isError, _ := raw["is_error"].(bool)
        evType := "stdout"
        if isError {
            evType = "stderr"
        }
        return Event{Type: evType, Content: content}
    case "error":
        content, _ := raw["content"].(string)
        return Event{Type: "error", Content: content}
    }

    // Legacy CLI stream-json format — unwrap ExecStream envelope if present.
    if _, hasStream := raw["stream"]; hasStream {
        content, _ := raw["content"].(string)
        if content == "" {
            return Event{}
        }
        var inner map[string]any
        if err := json.Unmarshal([]byte(content), &inner); err != nil {
            stream, _ := raw["stream"].(string)
            evType := "stdout"
            if stream == "stderr" {
                evType = "stderr"
            }
            return Event{Type: evType, Content: content}
        }
        raw = inner
        typ, _ = raw["type"].(string)
    }

    // Legacy event types.
    switch typ {
    case "result":
        return Event{Type: "complete", Output: raw}
    case "needs_input":
        return Event{Type: "needs_input", Content: fmt.Sprintf("%v", raw["message"])}
    case "system", "rate_limit_event":
        return Event{}
    case "user":
        return parseToolResult(raw)
    case "assistant":
        return parseAssistantMessage(raw)
    default:
        content, _ := raw["content"].(string)
        if content == "" {
            return Event{}
        }
        return Event{Type: "stdout", Content: content}
    }
}
```

- [ ] **Step 5: Run agent tests to confirm bridge tests now pass**

```bash
go test ./internal/agent/... -v
```

Expected: all pass, including the four new `TestParseClaudeEvent_Bridge*` tests.

---

### Task 4: Update ClaudeCodeRunner to use bridge

**Files:**
- Modify: `internal/agent/claudecode.go` — write prompt file, invoke bridge instead of `claude -p`

- [ ] **Step 1: Write failing tests for bridge invocation**

In `internal/agent/claudecode_test.go`, add tests for the bridge command structure and the `EvalPluginDirs` regression:

```go
// Verify the command string contains node bridge invocation.
func TestClaudeCodeRunner_UsesBridge(t *testing.T) {
    // This is a structural test — we inspect the command that would be executed.
    // The actual ExecStream is a no-op in tests.
    captured := ""
    sb := &capturingSandbox{onExecStream: func(cmd string) { captured = cmd }}
    r := NewClaudeCodeRunner(sb)
    _, _ = r.Run(context.Background(), "sb-1", RunOpts{
        Prompt:   "fix the bug",
        WorkDir:  "/workspace/repo",
        MaxTurns: 5,
    })
    assert.Contains(t, captured, "node /agent/bridge.js")
    assert.NotContains(t, captured, "claude -p")
}

type capturingSandbox struct {
    sandbox.Client // embed to satisfy interface; only ExecStream is used
    onExecStream func(cmd string)
}

func (c *capturingSandbox) Create(_ context.Context, _ sandbox.CreateOpts) (string, error) {
    return "sb-1", nil
}
func (c *capturingSandbox) ExecStream(_ context.Context, _, cmd, _ string, _ func(string)) error {
    if c.onExecStream != nil {
        c.onExecStream(cmd)
    }
    return nil
}
func (c *capturingSandbox) Exec(_ context.Context, _, _, _ string) (string, string, error) {
    return "", "", nil
}
func (c *capturingSandbox) WriteFile(_ context.Context, _, _, _ string) error         { return nil }
func (c *capturingSandbox) WriteBytes(_ context.Context, _, _ string, _ []byte) error { return nil }
func (c *capturingSandbox) ReadFile(_ context.Context, _, _ string) (string, error)   { return "", nil }
func (c *capturingSandbox) ReadBytes(_ context.Context, _, _ string) ([]byte, error)  { return nil, nil }
func (c *capturingSandbox) Kill(_ context.Context, _ string) error                    { return nil }
func (c *capturingSandbox) RenewExpiration(_ context.Context, _ string) error         { return nil }

// TestClaudeCodeRunner_EvalPluginDirsPassedToBridge verifies that EvalPluginDirs
// are forwarded to the bridge invocation. This is a regression test — the old CLI
// runner passed them as --plugin-dir flags; the bridge must propagate them too.
func TestClaudeCodeRunner_EvalPluginDirsPassedToBridge(t *testing.T) {
    captured := ""
    sb := &capturingSandbox{onExecStream: func(cmd string) { captured = cmd }}
    r := NewClaudeCodeRunner(sb)
    _, _ = r.Run(context.Background(), "sb-1", RunOpts{
        Prompt:         "fix",
        WorkDir:        "/workspace/repo",
        EvalPluginDirs: []string{"/agent/plugins/myplugin"},
    })
    assert.Contains(t, captured, "/agent/plugins/myplugin",
        "EvalPluginDirs must be forwarded to the bridge")
}
```

Run:
```bash
go test ./internal/agent/... -run "TestClaudeCodeRunner_UsesBridge|TestClaudeCodeRunner_EvalPluginDirs" -v
```
Expected: both FAIL (runner still invokes `claude -p`, plugin dirs not forwarded).

- [ ] **Step 2: Update ClaudeCodeRunner.Run to write prompt file and invoke bridge**

Replace the `Run` method in `internal/agent/claudecode.go`:

```go
func (r *ClaudeCodeRunner) Run(ctx context.Context, sandboxID string, opts RunOpts) (<-chan Event, error) {
    // Write prompt to a temp file to avoid shell arg length limits.
    promptFile := "/tmp/fleetlift-prompt.txt"
    if err := r.sandbox.WriteFile(ctx, sandboxID, promptFile, opts.Prompt); err != nil {
        return nil, fmt.Errorf("write prompt file: %w", err)
    }

    // If MCP sidecar is available, configure it before the bridge runs.
    mcpSetup := `. /tmp/fleetlift-mcp-env.sh 2>/dev/null; ` +
        `if [ -n "$FLEETLIFT_MCP_PORT" ]; then ` +
        `printf '{"mcpServers":{"fleetlift":{"type":"sse","url":"http://localhost:%s/sse"}}}' "$FLEETLIFT_MCP_PORT" > /workspace/.mcp.json; ` +
        `fi`

    // Pass EvalPluginDirs as extra args after maxTurns; bridge.js reads them as
    // additional MCP plugin directories (see Unanswered Question 2 for API details).
    pluginArgs := ""
    for _, dir := range opts.EvalPluginDirs {
        pluginArgs += " " + shellquote.Quote(dir)
    }

    cmd := fmt.Sprintf("%s && node /agent/bridge.js %s %s %d%s",
        mcpSetup,
        shellquote.Quote(promptFile),
        shellquote.Quote(opts.WorkDir),
        effectiveMaxTurns(opts.MaxTurns),
        pluginArgs,
    )

    ch := make(chan Event, 64)
    go func() {
        defer close(ch)
        err := r.sandbox.ExecStream(ctx, sandboxID, cmd, opts.WorkDir, func(line string) {
            event := parseClaudeEvent(line)
            select {
            case ch <- event:
            case <-ctx.Done():
            }
        })
        if err != nil {
            select {
            case ch <- Event{Type: "error", Content: err.Error()}:
            case <-ctx.Done():
            }
        }
    }()
    return ch, nil
}
```

Note: `EvalPluginDirs` (`--plugin-dir` flags) are a Claude Code CLI feature. If the bridge's `query()` API supports equivalent plugin loading, pass them through; otherwise note this as a known gap for now.

- [ ] **Step 3: Run all agent tests**

```bash
go test ./internal/agent/... -v
```

Expected: all pass including `TestClaudeCodeRunner_UsesBridge`.

- [ ] **Step 4: Run full test suite and lint**

```bash
go test ./... && make lint
```

Expected: all pass, no lint errors.

- [ ] **Step 5: Build the sandbox image to verify bridge.js is present**

```bash
docker build -f docker/Dockerfile.sandbox -t claude-code-sandbox:latest .
docker run --rm claude-code-sandbox:latest node /agent/bridge.js --help 2>&1 || true
docker run --rm claude-code-sandbox:latest test -f /agent/bridge.js && echo "bridge present"
```

Expected: `bridge present` (and a usage/error message from running without args).

- [ ] **Step 6: Commit**

```bash
git add docker/bridge.js docker/Dockerfile.sandbox \
        internal/agent/claudecode.go internal/agent/claudecode_test.go
git commit -m "feat: add Node.js bridge script for stable Claude Code event stream"
```

---

## Part C — Per-Step Thinking Toggle

### Task 5: Thread `show_thinking` from step config to bridge

`show_thinking: true` in a step's `execution:` block causes the bridge to emit Claude's thinking blocks as `thinking` stream events. Off by default — thinking is noisy and rarely needed outside debugging.

**Data flow:** `ExecutionDef.ShowThinking` → `resolveStep()` → `ResolvedStepOpts.ShowThinking` → `RunOpts.ShowThinking` → bridge positional arg 4 → bridge emits/suppresses `thinking` events.

**Files:**
- Modify: `internal/model/workflow.go` — `ExecutionDef`: add `ShowThinking bool`
- Modify: `internal/workflow/step.go` — `ResolvedStepOpts`: add `ShowThinking bool`
- Modify: `internal/workflow/dag.go` — `resolveStep()`: copy field through
- Modify: `internal/agent/runner.go` — `RunOpts`: add `ShowThinking bool`
- Modify: `internal/agent/claudecode.go` — pass arg to bridge; update command format
- Modify: `docker/bridge.js` — read arg, conditionally emit thinking blocks
- Test: `internal/agent/claudecode_test.go`
- Test: `internal/workflow/dag_test.go` (or `step_test.go`)

- [ ] **Step 1: Write failing tests**

In `internal/agent/claudecode_test.go`, add:

```go
func TestParseClaudeEvent_BridgeThinking(t *testing.T) {
    line := `{"type":"thinking","content":"Let me analyse the diff first..."}`
    ev := parseClaudeEvent(line)
    assert.Equal(t, "thinking", ev.Type)
    assert.Equal(t, "Let me analyse the diff first...", ev.Content)
}

func TestClaudeCodeRunner_ShowThinkingPassedToBridge(t *testing.T) {
    captured := ""
    sb := &capturingSandbox{onExecStream: func(cmd string) { captured = cmd }}
    r := NewClaudeCodeRunner(sb)
    _, _ = r.Run(context.Background(), "sb-1", RunOpts{
        Prompt:       "fix",
        WorkDir:      "/workspace/repo",
        ShowThinking: true,
    })
    assert.Contains(t, captured, "true",
        "ShowThinking=true must be forwarded as bridge arg")
}

func TestClaudeCodeRunner_ShowThinkingDefaultFalse(t *testing.T) {
    captured := ""
    sb := &capturingSandbox{onExecStream: func(cmd string) { captured = cmd }}
    r := NewClaudeCodeRunner(sb)
    _, _ = r.Run(context.Background(), "sb-1", RunOpts{
        Prompt:  "fix",
        WorkDir: "/workspace/repo",
        // ShowThinking not set — zero value is false
    })
    assert.Contains(t, captured, "false",
        "ShowThinking defaults to false")
}
```

Run:
```bash
go test ./internal/agent/... -run "TestParseClaudeEvent_BridgeThinking|TestClaudeCodeRunner_ShowThinking" -v
```
Expected: all FAIL.

- [ ] **Step 2: Add `ShowThinking` to `ExecutionDef` and `ResolvedStepOpts`**

In `internal/model/workflow.go`, add to `ExecutionDef`:

```go
type ExecutionDef struct {
    Agent        string           `yaml:"agent"`
    Prompt       string           `yaml:"prompt"`
    Verifiers    any              `yaml:"verifiers,omitempty"`
    Credentials  []string         `yaml:"credentials,omitempty"`
    Output       *OutputSchemaDef `yaml:"output,omitempty"`
    EvalPlugins  []string         `yaml:"eval_plugins,omitempty"`
    MaxTurns     int              `yaml:"max_turns,omitempty"`
    ShowThinking bool             `yaml:"show_thinking,omitempty"`
}
```

In `internal/workflow/step.go`, add to `ResolvedStepOpts`:

```go
type ResolvedStepOpts struct {
    Prompt            string                  `json:"prompt"`
    Repos             []model.RepoRef         `json:"repos"`
    Verifiers         any                     `json:"verifiers,omitempty"`
    Credentials       []string                `json:"credentials,omitempty"`
    PRConfig          *model.PRDef            `json:"pr_config,omitempty"`
    Agent             string                  `json:"agent"`
    MaxTurns          int                     `json:"max_turns,omitempty"`
    SandboxGroupImage string                  `json:"sandbox_group_image,omitempty"`
    EffectiveProfile  *model.AgentProfileBody `json:"effective_profile,omitempty"`
    EvalPluginURLs    []string                `json:"eval_plugin_urls,omitempty"`
    ShowThinking      bool                    `json:"show_thinking,omitempty"`
}
```

- [ ] **Step 3: Copy `ShowThinking` through `resolveStep()`**

In `internal/workflow/dag.go`, in `resolveStep()`, add after `opts.MaxTurns = step.Execution.MaxTurns`:

```go
opts.ShowThinking = step.Execution.ShowThinking
```

- [ ] **Step 4: Add `ShowThinking` to `RunOpts`**

In `internal/agent/runner.go`:

```go
type RunOpts struct {
    Prompt         string
    WorkDir        string
    MaxTurns       int
    Environment    map[string]string
    EvalPluginDirs []string
    ShowThinking   bool
}
```

- [ ] **Step 5: Pass `ShowThinking` to bridge in `ClaudeCodeRunner.Run`**

In `internal/agent/claudecode.go`, update the command construction to pass `showThinking` as the 4th positional arg (before plugin dirs):

```go
    // Pass showThinking as positional arg 4 so bridge.js can toggle thinking output.
    showThinkingArg := "false"
    if opts.ShowThinking {
        showThinkingArg = "true"
    }

    pluginArgs := ""
    for _, dir := range opts.EvalPluginDirs {
        pluginArgs += " " + shellquote.Quote(dir)
    }

    cmd := fmt.Sprintf("%s && node /agent/bridge.js %s %s %d %s%s",
        mcpSetup,
        shellquote.Quote(promptFile),
        shellquote.Quote(opts.WorkDir),
        effectiveMaxTurns(opts.MaxTurns),
        showThinkingArg,
        pluginArgs,
    )
```

- [ ] **Step 6: Update bridge.js to read `showThinking` and emit thinking blocks**

Update the argument destructuring at the top of `docker/bridge.js`:

```js
const [, , promptFile, workDir = '/workspace', maxTurnsStr = '100', showThinkingStr = 'false', ...pluginDirs] = process.argv;
const showThinking = showThinkingStr === 'true';
```

In the `assistant` case, add a thinking block handler after the `tool_use` block:

```js
            } else if (block.type === 'thinking' && block.thinking && showThinking) {
              emit({ type: 'thinking', content: block.thinking });
            }
```

- [ ] **Step 7: Add `thinking` case to `parseClaudeEvent`**

In `internal/agent/claudecode.go`, add to the bridge format switch:

```go
    case "thinking":
        content, _ := raw["content"].(string)
        return Event{Type: "thinking", Content: content}
```

Also add `"thinking"` as a valid stream type to `StepRunLog.Stream` comment in `internal/model/step.go` (it already stores `stdout | stderr | system` — `thinking` should be documented alongside):

```go
Stream    string    `db:"stream" json:"stream"` // stdout | stderr | system | thinking
```

- [ ] **Step 8: Add a `resolveStep` test for `ShowThinking`**

In `internal/workflow/dag_test.go` (or create `step_resolve_test.go`), find the existing `resolveStep` tests or add:

```go
func TestResolveStep_ShowThinking(t *testing.T) {
    step := model.StepDef{
        ID: "think",
        Execution: &model.ExecutionDef{
            Agent:        "claude-code",
            Prompt:       "analyse",
            ShowThinking: true,
        },
    }
    opts, err := resolveStep(step, nil, nil)
    require.NoError(t, err)
    assert.True(t, opts.ShowThinking)
}

func TestResolveStep_ShowThinkingDefaultFalse(t *testing.T) {
    step := model.StepDef{
        ID: "no-think",
        Execution: &model.ExecutionDef{
            Agent:  "claude-code",
            Prompt: "fix",
        },
    }
    opts, err := resolveStep(step, nil, nil)
    require.NoError(t, err)
    assert.False(t, opts.ShowThinking)
}
```

- [ ] **Step 9: Run full tests**

```bash
go test ./internal/agent/... ./internal/workflow/... -v 2>&1 | tail -40
go test ./... && make lint
```

Expected: all pass including the three new `ShowThinking` tests.

- [ ] **Step 10: Commit**

```bash
git add internal/model/workflow.go internal/workflow/step.go internal/workflow/dag.go \
        internal/agent/runner.go internal/agent/claudecode.go internal/agent/claudecode_test.go \
        docker/bridge.js
git commit -m "feat: add per-step show_thinking toggle threaded from ExecutionDef to bridge"
```

**YAML usage after this task:**
```yaml
steps:
  - id: analyse
    execution:
      agent: claude-code
      prompt: "Review the diff and explain your reasoning"
      show_thinking: true   # thinking blocks appear in the step log stream
```

---

## Unanswered Questions

1. **Bridge `query()` options** — Does `@anthropic-ai/claude-code`'s `query()` accept `dangerouslySkipPermissions` as an option? The CLI flag `--dangerously-skip-permissions` may not have a direct option equivalent. Verify in Task 2 Step 2 and adjust bridge.js if needed (may need `allowedTools: ['all']` or similar).

2. **Plugin dirs** — The current CLI invocation supports `--plugin-dir` flags for eval plugin loading (`RunOpts.EvalPluginDirs`). Bridge arg order is now: `promptFile workDir maxTurns showThinking [pluginDir...]`. Check whether the `query()` API supports equivalent MCP plugin directory loading (e.g. `pluginDirs` option); if not, document as a gap.

3. **`needs_input` event** — The CLI emits a `needs_input` event for HITL pausing. Verify whether the bridge's `query()` API surfaces this event type. If not, the HITL flow (`model.StepStatusAwaitingInput`) relies on the MCP sidecar path (DB query in `ExecuteStep`) which is independent of the event stream, so it may still work without this event.
