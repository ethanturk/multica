import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough, Writable } from "node:stream";
import test from "node:test";

import { runUiTestSmoke } from "./ui-test-mcp-smoke.mjs";

const SAFE_TOOLS = [
  "browser_navigate",
  "browser_snapshot",
  "browser_accessibility_scan",
  "browser_take_screenshot",
];

class FakeChild extends EventEmitter {
  constructor(handleRequest, { exitOnClose = true, stderr = "" } = {}) {
    super();
    this.stdout = new PassThrough();
    this.stderr = new PassThrough();
    this.requests = [];
    this.stdinEnded = false;
    this.kills = [];
    let pending = "";
    this.stdin = new Writable({
      write: (chunk, _encoding, callback) => {
        pending += chunk.toString();
        for (;;) {
          const newline = pending.indexOf("\n");
          if (newline < 0) break;
          const line = pending.slice(0, newline);
          pending = pending.slice(newline + 1);
          const request = JSON.parse(line);
          this.requests.push(request);
          handleRequest?.(request, this);
        }
        callback();
      },
      final: (callback) => {
        this.stdinEnded = true;
        if (exitOnClose) queueMicrotask(() => this.emit("close", 0, null));
        callback();
      },
    });
    if (stderr) queueMicrotask(() => this.stderr.write(stderr));
  }

  respond(request, result) {
    this.stdout.write(
      `${JSON.stringify({ jsonrpc: "2.0", id: request.id, result })}\n`,
    );
  }

  fail(
    request,
    code = -32000,
    message = "upstream failed",
    data = { class: "browser" },
  ) {
    this.stdout.write(
      `${JSON.stringify({
        jsonrpc: "2.0",
        id: request.id,
        error: { code, message, data },
      })}\n`,
    );
  }

  kill(signal) {
    this.kills.push(signal);
    queueMicrotask(() => this.emit("close", null, signal));
    return true;
  }
}

function successHandler(request, child) {
  if (request.method === "notifications/initialized") return;
  if (request.method === "initialize") {
    child.respond(request, {
      protocolVersion: "2025-11-25",
      capabilities: { tools: {} },
      serverInfo: { name: "fake-ui-test", version: "1" },
    });
    return;
  }
  if (request.method === "tools/list") {
    child.respond(request, {
      tools: SAFE_TOOLS.map((name) => ({
        name,
        description: `${name} fixture`,
        inputSchema: { type: "object" },
      })),
    });
    return;
  }
  if (request.method === "tools/call") {
    if (
      request.params.name === "browser_navigate" &&
      request.params.arguments.url === "https://example.com"
    ) {
      child.fail(request, -32602, "external target rejected", {
        class: "policy",
      });
      return;
    }
    child.respond(request, {
      content: [{ type: "text", text: `${request.params.name} completed` }],
      isError: false,
    });
  }
}

function smokeOptions(overrides = {}) {
  const children = [];
  const spawns = [];
  const stats = [];
  const removals = [];
  return {
    children,
    spawns,
    stats,
    removals,
    options: {
      cwd: "/repo",
      env: {
        MULTICA_UI_TEST_BIN: "/repo/bin/multica",
        MULTICA_UI_TEST_TASK_ID: "task-10",
        MULTICA_UI_TEST_RUNTIME_DIR:
          "/home/test/.multica/ui-test/runtimes/0.0.78",
      },
      readFile: async () =>
        JSON.stringify({
          start: "make start",
          url: "http://127.0.0.1:3000",
          health: "/login",
        }),
      removeFile: async (path, options) => {
        removals.push({ path, options });
      },
      statFile: async (path) => {
        stats.push(path);
        return { isFile: () => true, size: 123 };
      },
      spawnProcess: (binary, args, options) => {
        spawns.push({ binary, args, options });
        const child = new FakeChild(successHandler);
        children.push(child);
        return child;
      },
      requestTimeoutMs: 100,
      exitTimeoutMs: 100,
      ...overrides,
    },
  };
}

test("runs bounded MCP workflow in order and closes cleanly", async () => {
  const fixture = smokeOptions();

  const result = await runUiTestSmoke(fixture.options);

  assert.deepEqual(result, {
    artifactPath:
      "/repo/.multica/artifacts/ui-test/task-10/smoke-screenshot.png",
    tools: SAFE_TOOLS,
  });
  assert.deepEqual(fixture.spawns, [
    {
      binary: "/repo/bin/multica",
      args: ["ui-test", "serve"],
      options: {
        cwd: "/repo",
        env: fixture.options.env,
        shell: false,
        stdio: ["pipe", "pipe", "pipe"],
      },
    },
  ]);
  assert.deepEqual(
    fixture.children[0].requests.map(({ method, params }) => [
      method,
      params?.name ?? null,
    ]),
    [
      ["initialize", null],
      ["notifications/initialized", null],
      ["tools/list", null],
      ["tools/call", "browser_navigate"],
      ["tools/call", "browser_snapshot"],
      ["tools/call", "browser_accessibility_scan"],
      ["tools/call", "browser_take_screenshot"],
      ["tools/call", "browser_navigate"],
    ],
  );
  assert.equal(
    fixture.children[0].requests[3].params.arguments.url,
    "http://127.0.0.1:3000",
  );
  assert.deepEqual(fixture.children[0].requests[6].params.arguments, {
    type: "png",
    filename: "smoke-screenshot.png",
  });
  assert.deepEqual(fixture.stats, [result.artifactPath]);
  assert.deepEqual(fixture.removals, [
    { path: result.artifactPath, options: { force: true } },
  ]);
  assert.equal(fixture.children[0].stdinEnded, true);
  assert.deepEqual(fixture.children[0].kills, []);
});

test("selects an explicitly named ready runtime profile without installing", async () => {
  const fixture = smokeOptions();
  fixture.options.env = {
    ...fixture.options.env,
    MULTICA_UI_TEST_PROFILE: "ui-test-integration",
  };

  await runUiTestSmoke(fixture.options);

  assert.deepEqual(fixture.spawns[0].args, [
    "--profile",
    "ui-test-integration",
    "ui-test",
    "serve",
  ]);
});

test("rejects advertised hidden tools and still closes child input", async () => {
  const fixture = smokeOptions({
    spawnProcess: () => {
      const child = new FakeChild((request, current) => {
        if (request.method === "notifications/initialized") return;
        if (request.method === "initialize") {
          current.respond(request, {
            protocolVersion: "2025-11-25",
            capabilities: { tools: {} },
            serverInfo: { name: "fake-ui-test", version: "1" },
          });
        } else {
          current.respond(request, {
            tools: [
              ...SAFE_TOOLS.map((name) => ({
                name,
                description: name,
                inputSchema: { type: "object" },
              })),
              {
                name: "browser_run_code_unsafe",
                description: "unsafe",
                inputSchema: { type: "object" },
              },
            ],
          });
        }
      });
      fixture.children.push(child);
      return child;
    },
  });

  await assert.rejects(
    runUiTestSmoke(fixture.options),
    /unsafe browser tools were advertised/,
  );
  assert.equal(fixture.children[0].stdinEnded, true);
});

test("requires external navigation to be rejected", async () => {
  const fixture = smokeOptions({
    spawnProcess: () => {
      const child = new FakeChild((request, current) => {
        if (
          request.method === "tools/call" &&
          request.params.name === "browser_navigate" &&
          request.params.arguments.url === "https://example.com"
        ) {
          current.respond(request, {
            content: [{ type: "text", text: "unexpectedly navigated" }],
            isError: false,
          });
        } else {
          successHandler(request, current);
        }
      });
      fixture.children.push(child);
      return child;
    },
  });

  await assert.rejects(
    runUiTestSmoke(fixture.options),
    /external navigation was not rejected/,
  );
  assert.equal(fixture.children[0].stdinEnded, true);
});

test("does not mistake external request failures for policy rejection", async (t) => {
  const scenarios = [
    {
      name: "timeout",
      handleExternal() {},
      pattern: /external navigation was not rejected/,
    },
    {
      name: "browser-class invalid params",
      handleExternal(request, child) {
        child.fail(request, -32602, "browser rejected request", {
          class: "browser",
        });
      },
      pattern: /external navigation was not rejected/,
    },
    {
      name: "policy-class generic upstream error",
      handleExternal(request, child) {
        child.fail(request, -32000, "generic upstream error", {
          class: "policy",
        });
      },
      pattern: /external navigation was not rejected/,
    },
    {
      name: "malformed response",
      handleExternal(_request, child) {
        child.stdout.write("not-json\n");
      },
      pattern: /UI-test MCP returned malformed JSON/,
    },
  ];

  for (const scenario of scenarios) {
    await t.test(scenario.name, async () => {
      const fixture = smokeOptions({
        spawnProcess: () => {
          const child = new FakeChild((request, current) => {
            if (
              request.method === "tools/call" &&
              request.params.name === "browser_navigate" &&
              request.params.arguments.url === "https://example.com"
            ) {
              scenario.handleExternal(request, current);
            } else {
              successHandler(request, current);
            }
          });
          fixture.children.push(child);
          return child;
        },
        requestTimeoutMs: 20,
      });

      await assert.rejects(runUiTestSmoke(fixture.options), scenario.pattern);
      assert.equal(fixture.children[0].stdinEnded, true);
    });
  }
});

test("does not accept a stale screenshot from an earlier run", async () => {
  let screenshotExists = true;
  const removals = [];
  const fixture = smokeOptions({
    removeFile: async (path, options) => {
      removals.push({ path, options });
      screenshotExists = false;
    },
    statFile: async () => {
      if (!screenshotExists) throw new Error("missing");
      return { isFile: () => true, size: 123 };
    },
  });

  await assert.rejects(
    runUiTestSmoke(fixture.options),
    /UI-test screenshot was not created/,
  );
  assert.deepEqual(removals, [
    {
      path: "/repo/.multica/artifacts/ui-test/task-10/smoke-screenshot.png",
      options: { force: true },
    },
  ]);
});

test("does not expose screenshot filesystem diagnostics", async () => {
  const fixture = smokeOptions({
    statFile: async () => {
      throw new Error("token=top-secret-storage-state");
    },
  });

  await assert.rejects(
    runUiTestSmoke(fixture.options),
    /^Error: UI-test screenshot was not created$/,
  );
  assert.equal(fixture.children[0].stdinEnded, true);
});

test("bounds timeout, malformed, and JSON-RPC error diagnostics", async (t) => {
  const cases = [
    {
      name: "timeout",
      handler(request, child) {
        if (request.method !== "initialize") successHandler(request, child);
      },
      pattern: /^MCP request timed out: initialize$/,
    },
    {
      name: "malformed",
      handler(request, child) {
        if (request.method === "initialize") child.stdout.write("not-json\n");
      },
      pattern: /^UI-test MCP returned malformed JSON$/,
    },
    {
      name: "JSON-RPC error",
      handler(request, child) {
        if (request.method === "initialize") {
          child.fail(request, -32000, `token=${"x".repeat(200_000)}`);
        }
      },
      pattern: /^MCP request failed: initialize \(-32000\)$/,
    },
  ];

  for (const scenario of cases) {
    await t.test(scenario.name, async () => {
      const secret = "top-secret-cookie";
      const fixture = smokeOptions({
        spawnProcess: () => {
          const child = new FakeChild(scenario.handler, {
            stderr: `${secret}${"s".repeat(200_000)}`,
          });
          fixture.children.push(child);
          return child;
        },
        requestTimeoutMs: 20,
      });

      let error;
      try {
        await runUiTestSmoke(fixture.options);
      } catch (caught) {
        error = caught;
      }
      assert.ok(error instanceof Error);
      assert.match(error.message, scenario.pattern);
      assert.equal(error.message.includes(secret), false);
      assert.ok(error.message.length < 200);
      assert.equal(fixture.children[0].stdinEnded, true);
    });
  }
});

test("kills a child that does not exit after stdin closes", async () => {
  const fixture = smokeOptions({
    spawnProcess: () => {
      const child = new FakeChild(successHandler, { exitOnClose: false });
      fixture.children.push(child);
      return child;
    },
    exitTimeoutMs: 20,
  });

  await assert.rejects(
    runUiTestSmoke(fixture.options),
    /UI-test MCP process did not exit cleanly/,
  );
  assert.equal(fixture.children[0].stdinEnded, true);
  assert.deepEqual(fixture.children[0].kills, ["SIGTERM"]);
});

test("rejects a clean child exit before the client closes stdin", async () => {
  const fixture = smokeOptions({
    spawnProcess: () => {
      const child = new FakeChild((request, current) => {
        successHandler(request, current);
        if (
          request.method === "tools/call" &&
          request.params.name === "browser_navigate" &&
          request.params.arguments.url === "https://example.com"
        ) {
          current.emit("close", 0, null);
        }
      });
      fixture.children.push(child);
      return child;
    },
  });

  await assert.rejects(
    runUiTestSmoke(fixture.options),
    /UI-test MCP process exited unexpectedly/,
  );
  assert.equal(fixture.children[0].stdinEnded, true);
});

test("rejects missing or unsafe caller configuration before spawning", async () => {
  for (const [env, pattern] of [
    [{}, /MULTICA_UI_TEST_BIN is required/],
    [
      { MULTICA_UI_TEST_BIN: "/bin/multica" },
      /MULTICA_UI_TEST_TASK_ID is required/,
    ],
    [
      {
        MULTICA_UI_TEST_BIN: "/bin/multica",
        MULTICA_UI_TEST_TASK_ID: "../escape",
      },
      /MULTICA_UI_TEST_TASK_ID is invalid/,
    ],
    [
      {
        MULTICA_UI_TEST_BIN: "/bin/multica",
        MULTICA_UI_TEST_TASK_ID: "safe",
      },
      /MULTICA_UI_TEST_RUNTIME_DIR is required/,
    ],
  ]) {
    let spawned = false;
    await assert.rejects(
      runUiTestSmoke({
        cwd: "/repo",
        env,
        spawnProcess: () => {
          spawned = true;
        },
      }),
      pattern,
    );
    assert.equal(spawned, false);
  }
});
