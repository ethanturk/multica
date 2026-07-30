import { spawn } from "node:child_process";
import { readFile, rm, stat } from "node:fs/promises";
import { isIP } from "node:net";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const MAX_FRAME_BYTES = 1024 * 1024;
const MAX_REQUESTS = 32;
const DEFAULT_REQUEST_TIMEOUT_MS = 30_000;
const DEFAULT_EXIT_TIMEOUT_MS = 10_000;

const ALLOWED_TOOLS = new Set([
  "browser_accessibility_scan",
  "browser_click",
  "browser_console_messages",
  "browser_drag",
  "browser_drop",
  "browser_fill_form",
  "browser_find",
  "browser_hover",
  "browser_navigate",
  "browser_navigate_back",
  "browser_network_request",
  "browser_network_requests",
  "browser_press_key",
  "browser_resize",
  "browser_select_option",
  "browser_snapshot",
  "browser_tabs",
  "browser_take_screenshot",
  "browser_type",
  "browser_wait_for",
]);

const REQUIRED_TOOLS = [
  "browser_navigate",
  "browser_snapshot",
  "browser_accessibility_scan",
  "browser_take_screenshot",
];

function required(env, name) {
  const value = typeof env[name] === "string" ? env[name].trim() : "";
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function loopbackTarget(raw) {
  const normalized = typeof raw === "string" ? raw.trim() : "";
  let target;
  try {
    target = new URL(normalized);
  } catch {
    throw new Error("UI-test manifest URL is invalid");
  }
  const hostname = target.hostname.toLowerCase().replace(/^\[|\]$/g, "");
  const loopback =
    hostname === "localhost" ||
    (isIP(hostname) === 4 && hostname.startsWith("127.")) ||
    (isIP(hostname) === 6 && hostname === "::1");
  if (
    !["http:", "https:"].includes(target.protocol) ||
    !loopback ||
    target.username ||
    target.password ||
    target.hash
  ) {
    throw new Error("UI-test manifest URL must be loopback HTTP");
  }
  return normalized;
}

function withTimeout(promise, timeoutMs, message) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(message)), timeoutMs);
    }),
  ]).finally(() => clearTimeout(timer));
}

class MCPRequestError extends Error {
  constructor(method, rpcError) {
    const rpcCode = Number.isInteger(rpcError?.code) ? rpcError.code : -32000;
    super(`MCP request failed: ${method} (${rpcCode})`);
    this.name = "MCPRequestError";
    this.rpcCode = rpcCode;
    this.rpcClass =
      typeof rpcError?.data?.class === "string" ? rpcError.data.class : "";
  }
}

class MCPClient {
  constructor(child, requestTimeoutMs) {
    this.child = child;
    this.requestTimeoutMs = requestTimeoutMs;
    this.nextId = 1;
    this.pending = new Map();
    this.buffer = "";
    this.protocolError = null;
    this.closing = false;
    this.stderrBytes = 0;
    this.exit = new Promise((resolveExit) => {
      child.once("close", (code, signal) => resolveExit({ code, signal }));
    });

    child.stdout.on("data", (chunk) => this.consume(chunk));
    child.stderr.on("data", (chunk) => {
      this.stderrBytes = Math.min(
        MAX_FRAME_BYTES,
        this.stderrBytes + Buffer.byteLength(chunk),
      );
    });
    child.once("error", () => {
      this.failAll(new Error("UI-test MCP process failed to start"));
    });
    child.once("close", () => {
      if (!this.closing) {
        this.failAll(new Error("UI-test MCP process exited unexpectedly"));
      }
    });
  }

  consume(chunk) {
    if (this.protocolError) return;
    this.buffer += chunk.toString("utf8");
    if (Buffer.byteLength(this.buffer) > MAX_FRAME_BYTES) {
      this.failAll(new Error("UI-test MCP response exceeded size limit"));
      return;
    }
    for (;;) {
      const newline = this.buffer.indexOf("\n");
      if (newline < 0) return;
      const line = this.buffer.slice(0, newline);
      this.buffer = this.buffer.slice(newline + 1);
      if (!line.trim()) continue;
      let message;
      try {
        message = JSON.parse(line);
      } catch {
        this.failAll(new Error("UI-test MCP returned malformed JSON"));
        return;
      }
      if (message.id === undefined) continue;
      const pending = this.pending.get(message.id);
      if (!pending) {
        this.failAll(new Error("UI-test MCP returned an unknown response ID"));
        return;
      }
      this.pending.delete(message.id);
      clearTimeout(pending.timer);
      if (message.error) {
        pending.reject(new MCPRequestError(pending.method, message.error));
      } else if (!Object.hasOwn(message, "result")) {
        pending.reject(new Error("UI-test MCP response is missing a result"));
      } else {
        pending.resolve(message.result);
      }
    }
  }

  failAll(error) {
    if (!this.protocolError) this.protocolError = error;
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(this.protocolError);
    }
    this.pending.clear();
  }

  write(message) {
    if (this.protocolError) throw this.protocolError;
    const frame = `${JSON.stringify(message)}\n`;
    if (Buffer.byteLength(frame) > MAX_FRAME_BYTES) {
      throw new Error("UI-test MCP request exceeded size limit");
    }
    this.child.stdin.write(frame);
  }

  notify(method, params = {}) {
    this.write({ jsonrpc: "2.0", method, params });
  }

  request(method, params = {}) {
    if (this.nextId > MAX_REQUESTS) {
      return Promise.reject(new Error("UI-test MCP request limit exceeded"));
    }
    const id = this.nextId++;
    return new Promise((resolveRequest, rejectRequest) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        rejectRequest(new Error(`MCP request timed out: ${method}`));
      }, this.requestTimeoutMs);
      this.pending.set(id, {
        method,
        resolve: resolveRequest,
        reject: rejectRequest,
        timer,
      });
      try {
        this.write({ jsonrpc: "2.0", id, method, params });
      } catch (error) {
        clearTimeout(timer);
        this.pending.delete(id);
        rejectRequest(error);
      }
    });
  }

  async close(exitTimeoutMs) {
    const priorProtocolError = this.protocolError;
    this.closing = true;
    this.failAll(new Error("UI-test MCP client closed"));
    this.child.stdin.end();
    try {
      const exit = await withTimeout(
        this.exit,
        exitTimeoutMs,
        "UI-test MCP process did not exit cleanly",
      );
      if (exit.code !== 0 || exit.signal !== null) {
        throw new Error("UI-test MCP process did not exit cleanly");
      }
    } catch {
      this.child.kill("SIGTERM");
      await withTimeout(
        this.exit,
        exitTimeoutMs,
        "UI-test MCP process did not exit after termination",
      ).catch(() => {});
      throw new Error("UI-test MCP process did not exit cleanly");
    }
    if (priorProtocolError) throw priorProtocolError;
  }
}

async function callTool(client, name, argumentsValue) {
  const result = await client.request("tools/call", {
    name,
    arguments: argumentsValue,
  });
  if (
    result?.isError === true ||
    !Array.isArray(result?.content) ||
    result.content.length === 0
  ) {
    throw new Error(`UI-test tool failed: ${name}`);
  }
  return result;
}

export async function runUiTestSmoke({
  cwd = process.cwd(),
  env = process.env,
  spawnProcess = spawn,
  readFile: readManifest = readFile,
  removeFile = rm,
  statFile = stat,
  requestTimeoutMs = DEFAULT_REQUEST_TIMEOUT_MS,
  exitTimeoutMs = DEFAULT_EXIT_TIMEOUT_MS,
} = {}) {
  const binary = required(env, "MULTICA_UI_TEST_BIN");
  const taskId = required(env, "MULTICA_UI_TEST_TASK_ID");
  if (!/^[a-z0-9]+(?:[-_][a-z0-9]+)*$/i.test(taskId)) {
    throw new Error("MULTICA_UI_TEST_TASK_ID is invalid");
  }
  required(env, "MULTICA_UI_TEST_RUNTIME_DIR");
  const profile = env.MULTICA_UI_TEST_PROFILE?.trim() ?? "";
  if (profile && !/^[a-z0-9]+(?:[-_][a-z0-9]+)*$/i.test(profile)) {
    throw new Error("MULTICA_UI_TEST_PROFILE is invalid");
  }

  let manifest;
  try {
    manifest = JSON.parse(
      await readManifest(join(cwd, ".multica", "ui-test.json"), "utf8"),
    );
  } catch {
    throw new Error("UI-test manifest is unreadable");
  }
  const target = loopbackTarget(manifest?.url);
  const artifactPath = resolve(
    cwd,
    ".multica",
    "artifacts",
    "ui-test",
    taskId,
    "smoke-screenshot.png",
  );

  let child;
  try {
    const args = profile
      ? ["--profile", profile, "ui-test", "serve"]
      : ["ui-test", "serve"];
    child = spawnProcess(binary, args, {
      cwd,
      env,
      shell: false,
      stdio: ["pipe", "pipe", "pipe"],
    });
  } catch {
    throw new Error("UI-test MCP process failed to start");
  }
  const client = new MCPClient(child, requestTimeoutMs);
  let result;
  let workflowError;
  try {
    await client.request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "multica-ui-test-smoke", version: "1" },
    });
    client.notify("notifications/initialized");

    const listed = await client.request("tools/list");
    if (!Array.isArray(listed?.tools)) {
      throw new Error("UI-test MCP tools/list result is invalid");
    }
    const tools = listed.tools.map((tool) => tool?.name);
    if (
      tools.some(
        (name) => typeof name !== "string" || !ALLOWED_TOOLS.has(name),
      )
    ) {
      throw new Error("unsafe browser tools were advertised");
    }
    for (const name of REQUIRED_TOOLS) {
      if (!tools.includes(name)) {
        throw new Error(`required UI-test tool is missing: ${name}`);
      }
    }

    await callTool(client, "browser_navigate", { url: target });
    await callTool(client, "browser_snapshot", {});
    await callTool(client, "browser_accessibility_scan", {});
    try {
      await removeFile(artifactPath, { force: true });
    } catch {
      throw new Error("UI-test stale screenshot could not be removed");
    }
    await callTool(client, "browser_take_screenshot", {
      type: "png",
      filename: "smoke-screenshot.png",
    });
    let screenshot;
    try {
      screenshot = await statFile(artifactPath);
    } catch {
      throw new Error("UI-test screenshot was not created");
    }
    if (!screenshot.isFile() || screenshot.size <= 0) {
      throw new Error("UI-test screenshot was not created");
    }

    let externalRejected = false;
    try {
      await client.request("tools/call", {
        name: "browser_navigate",
        arguments: { url: "https://example.com" },
      });
    } catch (error) {
      externalRejected =
        error instanceof MCPRequestError &&
        error.rpcCode === -32602 &&
        error.rpcClass === "policy";
    }
    if (!externalRejected) {
      throw new Error("external navigation was not rejected");
    }
    result = { artifactPath, tools };
  } catch (error) {
    workflowError = error;
  }

  let cleanupError;
  try {
    await client.close(exitTimeoutMs);
  } catch (error) {
    cleanupError = error;
  }
  if (cleanupError) throw cleanupError;
  if (workflowError) throw workflowError;
  return result;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  runUiTestSmoke()
    .then(() => process.stdout.write("UI-test MCP smoke passed\n"))
    .catch((error) => {
      process.stderr.write(`${error.message}\n`);
      process.exitCode = 1;
    });
}
