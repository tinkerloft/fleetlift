#!/usr/bin/env node

import { spawn } from "node:child_process";
import fs from "node:fs/promises";

const CHUNK_SIZE = 64 * 1024;

function emit(event) {
  process.stdout.write(`${JSON.stringify(event)}\n`);
}

function usage() {
  process.stdout.write("Usage: node /agent/bridge.js <fleetlift-request.json>\n");
}

function validateRequest(req) {
  if (!req || typeof req !== "object") {
    throw new Error("request must be a JSON object");
  }
  if (req.version !== 1) {
    throw new Error(`unsupported request version: ${req.version}`);
  }
  if (!req.prompt_file || typeof req.prompt_file !== "string") {
    throw new Error("request.prompt_file is required");
  }
  if (!req.work_dir || typeof req.work_dir !== "string") {
    throw new Error("request.work_dir is required");
  }
  if (typeof req.max_turns !== "number" || req.max_turns < 1) {
    throw new Error("request.max_turns must be a positive number");
  }
  if (req.plugin_dirs && !Array.isArray(req.plugin_dirs)) {
    throw new Error("request.plugin_dirs must be an array");
  }
  if (req.model !== undefined && typeof req.model !== "string") {
    throw new Error("request.model must be a string");
  }
}

function toText(content) {
  if (typeof content === "string") {
    return content;
  }
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === "string") {
          return part;
        }
        if (part && typeof part === "object" && part.type === "text" && typeof part.text === "string") {
          return part.text;
        }
        return JSON.stringify(part);
      })
      .join("");
  }
  if (content === null || content === undefined) {
    return "";
  }
  if (typeof content === "object") {
    return JSON.stringify(content);
  }
  return String(content);
}

function emitToolResultChunks(callID, stream, content) {
  const text = content ?? "";
  const total = Math.max(1, Math.ceil(text.length / CHUNK_SIZE));
  for (let i = 0; i < total; i += 1) {
    const start = i * CHUNK_SIZE;
    const end = start + CHUNK_SIZE;
    emit({
      type: "tool_result",
      call_id: callID,
      stream,
      chunk_index: i + 1,
      chunk_total: total,
      content: text.slice(start, end),
    });
  }
}

function mapClaudeEvent(raw, state) {
  if (!raw || typeof raw !== "object") {
    return;
  }

  if (raw.type === "system" || raw.type === "rate_limit_event") {
    return;
  }

  if (raw.type === "needs_input") {
    emit({ type: "needs_input", content: String(raw.message ?? "Input required") });
    return;
  }

  if (raw.type === "result") {
    state.sawComplete = true;
    emit({
      type: "complete",
      result: typeof raw.result === "string" ? raw.result : "",
      subtype: raw.subtype,
      session_id: raw.session_id,
      is_error: raw.is_error === true,
      cost_usd: raw.cost_usd,
      total_cost_usd: raw.total_cost_usd,
      duration_ms: raw.duration_ms,
      num_turns: raw.num_turns,
    });
    return;
  }

  if (raw.type === "assistant") {
    const blocks = raw?.message?.content;
    if (!Array.isArray(blocks)) {
      return;
    }

    for (const block of blocks) {
      if (!block || typeof block !== "object") {
        continue;
      }

      if (block.type === "text" && typeof block.text === "string" && block.text !== "") {
        emit({ type: "assistant_text", content: block.text });
        continue;
      }

      if (block.type === "thinking" && typeof block.thinking === "string" && block.thinking !== "") {
        emit({ type: "assistant_text", content: block.thinking });
        continue;
      }

      if (block.type === "tool_use") {
        emit({
          type: "tool_call",
          call_id: block.id ?? "",
          name: typeof block.name === "string" ? block.name : "",
          description: typeof block?.input?.description === "string" ? block.input.description : "",
          command: typeof block?.input?.command === "string" ? block.input.command : "",
        });
      }
    }
    return;
  }

  if (raw.type === "user") {
    const blocks = raw?.message?.content;
    if (!Array.isArray(blocks)) {
      return;
    }

    for (const block of blocks) {
      if (!block || typeof block !== "object") {
        continue;
      }
      if (block.type !== "tool_result") {
        continue;
      }
      const stream = block.is_error === true ? "stderr" : "stdout";
      emitToolResultChunks(block.tool_use_id ?? "", stream, toText(block.content));
    }
    return;
  }

  if (typeof raw.content === "string" && raw.content !== "") {
    emit({ type: "status", content: raw.content });
  }
}

function streamLines(stream, onLine) {
  let buffer = "";
  stream.setEncoding("utf8");
  stream.on("data", (chunk) => {
    buffer += chunk;
    const lines = buffer.split("\n");
    buffer = lines.pop() ?? "";
    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed !== "") {
        onLine(trimmed);
      }
    }
  });
  stream.on("end", () => {
    const trimmed = buffer.trim();
    if (trimmed !== "") {
      onLine(trimmed);
    }
  });
}

async function main() {
  const arg = process.argv[2];
  if (!arg || arg === "--help" || arg === "-h") {
    usage();
    process.exit(arg ? 0 : 1);
  }

  let request;
  try {
    const raw = await fs.readFile(arg, "utf8");
    request = JSON.parse(raw);
    validateRequest(request);
  } catch (err) {
    emit({ type: "error", content: `invalid request: ${err.message}` });
    process.exit(1);
  }

  let prompt;
  try {
    prompt = await fs.readFile(request.prompt_file, "utf8");
  } catch (err) {
    emit({ type: "error", content: `failed to read prompt_file: ${err.message}` });
    process.exit(1);
  }

  const args = [
    "-p",
    prompt,
    "--output-format",
    "stream-json",
    "--verbose",
    "--dangerously-skip-permissions",
    "--max-turns",
    String(request.max_turns),
  ];

  if (typeof request.model === "string" && request.model !== "") {
    args.push("--model", request.model);
  }

  if (Array.isArray(request.plugin_dirs)) {
    for (const pluginDir of request.plugin_dirs) {
      if (typeof pluginDir === "string" && pluginDir !== "") {
        args.push("--plugin-dir", pluginDir);
      }
    }
  }

  const child = spawn("claude", args, {
    cwd: request.work_dir,
    env: {
      ...process.env,
      ...(request.env && typeof request.env === "object" ? request.env : {}),
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  const state = { sawComplete: false };

  streamLines(child.stdout, (line) => {
    try {
      const raw = JSON.parse(line);
      mapClaudeEvent(raw, state);
    } catch {
      emit({ type: "status", content: line });
    }
  });

  streamLines(child.stderr, (line) => {
    emit({ type: "status", content: line });
  });

  child.on("error", (err) => {
    emit({ type: "error", content: `failed to start claude: ${err.message}` });
  });

  child.on("close", (code) => {
    if (code !== 0 && !state.sawComplete) {
      emit({ type: "error", content: `claude exited with code ${code}` });
      process.exit(code ?? 1);
      return;
    }
    if (!state.sawComplete) {
      emit({ type: "error", content: "claude exited without completion event" });
      process.exit(1);
      return;
    }
    process.exit(0);
  });
}

void main();
