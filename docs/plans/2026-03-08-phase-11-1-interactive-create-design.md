# Phase 11.1: Interactive Create Command — Design

**Date:** 2026-03-08
**Status:** Approved

## Goal

Add `fleetlift create --interactive` (`-i`): a multi-turn conversation with Claude that gathers task requirements through natural dialogue and produces a task YAML file.

## Approach

Multi-turn messages API with a generation marker. A `[]anthropic.MessageParam` history is kept in memory. Claude asks questions one at a time; when it has enough info it outputs `---YAML---` followed by the task YAML. The CLI detects the marker, extracts the YAML, and hands off to the existing `confirmAndSave` flow.

## Architecture

- `--interactive` / `-i` flag on `createCmd`
- `runCreate` branches: if `--interactive`, call `runInteractiveCreate`; otherwise existing one-shot path unchanged
- `runInteractiveCreate` owns the conversation loop and history slice
- All downstream handling (validate, confirmAndSave, startRunFromFile) reused unchanged

## System Prompt

Reuses schema + examples from `buildSystemPrompt()`, extended with:
- Ask one question at a time; cover mode, repos, execution, verifiers
- When ready, output exactly `---YAML---\n<yaml>` with no prose after the marker

## Generation Signal

`strings.Cut(response, "---YAML---")` extracts the YAML half. `extractYAML()` then strips any markdown fences as a safety net.

## UX

```
$ fleetlift create --interactive

Claude: Hi! Let's build a Fleetlift task. What do you want the agent to do?

You: Migrate all our Go services from logrus to slog

Claude: What repositories should this run against?
...
Claude: I have enough to generate the task.
---YAML---
version: 1
...
```

- Prompt prefix: `You: ` (stdout)
- Response prefix: `\nClaude: `
- EOF (Ctrl+D) → `"Session ended."` and clean exit
- `--output` and `--run` flags work identically to one-shot mode

## Error Handling

| Scenario | Behavior |
|----------|----------|
| API error | Print error, exit non-zero |
| EOF / Ctrl+D | Print "Session ended.", exit 0 |
| Malformed YAML after marker | validateTaskYAML warning; user can still edit via confirmAndSave |
| User Ctrl+C | OS signal, no special handling needed |

## Testing

- `hasGenerationMarker(response string) bool` — unit tested
- `extractYAMLFromMarker(response string) string` — unit tested
- Conversation loop is integration-level; no API mocking needed in unit tests
