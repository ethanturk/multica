import assert from "node:assert/strict";
import {
  chmod,
  mkdtemp,
  readFile,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  buildStorageState,
  deriveUiTestIdentity,
  formatSetupFailure,
  formatSetupSuccess,
  readSetupConfig,
  resolvePlaywrightBaseUrl,
  writeStorageStateSecurely,
} from "./ui-test-setup-lib.mjs";

test("task ID produces a deterministic safe email and workspace slug", () => {
  const first = deriveUiTestIdentity(" Task/ABC 42 ");
  const second = deriveUiTestIdentity(" Task/ABC 42 ");

  assert.deepEqual(first, second);
  assert.deepEqual(first, {
    email: "ui-test-task-abc-42@multica.ai",
    workspaceName: "UI Test task-abc-42",
    workspaceSlug: "ui-test-task-abc-42",
  });
  assert.match(first.workspaceSlug, /^[a-z0-9]+(?:-[a-z0-9]+)*$/);
});

test("task ID rejects empty, symbol-only, and overlong values", () => {
  assert.throws(() => deriveUiTestIdentity(""), /task ID is required/);
  assert.throws(
    () => deriveUiTestIdentity("🔥 / !!!"),
    /task ID must contain a letter or digit/,
  );
  assert.throws(
    () => deriveUiTestIdentity("x".repeat(64)),
    /task ID is too long/,
  );
});

test("storage state contains only managed Multica local storage", () => {
  assert.deepEqual(
    buildStorageState({
      baseUrl: "http://127.0.0.1:3000/path",
      token: "top-secret-token",
    }),
    {
      cookies: [],
      origins: [
        {
          origin: "http://127.0.0.1:3000",
          localStorage: [
            { name: "multica_token", value: "top-secret-token" },
            { name: "multica:chat:isOpen", value: "false" },
          ],
        },
      ],
    },
  );
});

test("storage state rejects missing token and base URL", () => {
  assert.throws(
    () => buildStorageState({ baseUrl: "http://127.0.0.1:3000", token: "" }),
    /token is required/,
  );
  assert.throws(
    () => buildStorageState({ baseUrl: "", token: "top-secret-token" }),
    /base URL is required/,
  );
});

test("managed storage state rejects external and credentialed origins", () => {
  assert.throws(
    () =>
      buildStorageState({
        baseUrl: "https://example.com",
        token: "top-secret-token",
      }),
    /loopback without credentials or fragments/,
  );
  assert.throws(
    () =>
      buildStorageState({
        baseUrl: "http://user:password@127.0.0.1:3000",
        token: "top-secret-token",
      }),
    /loopback without credentials or fragments/,
  );
  assert.throws(
    () =>
      buildStorageState({
        baseUrl: "http://127.0.0.1:3000/#secret",
        token: "top-secret-token",
      }),
    /loopback without credentials or fragments/,
  );
});

test("managed storage state accepts HTTP loopback hosts", () => {
  for (const [baseUrl, origin] of [
    ["http://localhost:3000/path", "http://localhost:3000"],
    ["https://127.0.0.2:8443", "https://127.0.0.2:8443"],
    ["https://[::1]:8443", "https://[::1]:8443"],
  ]) {
    assert.equal(
      buildStorageState({
        baseUrl,
        token: "top-secret-token",
      }).origins[0].origin,
      origin,
    );
  }
});

test("setup config rejects missing storage-state path", () => {
  assert.throws(
    () =>
      readSetupConfig({
        MULTICA_UI_TEST_TASK_ID: "dogfood",
        MULTICA_UI_TEST_BASE_URL: "http://127.0.0.1:3000",
      }),
    /storage-state path is required/,
  );
});

test("managed storage-state origin also drives Playwright base URL", () => {
  assert.equal(
    resolvePlaywrightBaseUrl({
      MULTICA_UI_TEST_BASE_URL: "http://127.0.0.1:3000",
      PLAYWRIGHT_BASE_URL: "http://localhost:3000",
      FRONTEND_ORIGIN: "http://localhost:3001",
    }),
    "http://127.0.0.1:3000",
  );
  assert.equal(
    resolvePlaywrightBaseUrl({
      PLAYWRIGHT_BASE_URL: "http://localhost:3000",
    }),
    "http://localhost:3000",
  );
  assert.equal(
    resolvePlaywrightBaseUrl(
      {
        MULTICA_UI_TEST_STORAGE_STATE:
          ".multica/ui-test/dogfood/storage-state.json",
      },
      () => ({
        cookies: [],
        origins: [
          {
            origin: "http://127.0.0.1:3000",
            localStorage: [
              { name: "multica_token", value: "top-secret-token" },
              { name: "multica:chat:isOpen", value: "false" },
            ],
          },
        ],
      }),
    ),
    "http://127.0.0.1:3000",
  );
});

test("Playwright rejects external managed origins", () => {
  assert.throws(
    () =>
      resolvePlaywrightBaseUrl({
        MULTICA_UI_TEST_BASE_URL: "https://example.com",
      }),
    /loopback without credentials or fragments/,
  );
  assert.throws(
    () =>
      resolvePlaywrightBaseUrl(
        {
          MULTICA_UI_TEST_STORAGE_STATE:
            ".multica/ui-test/dogfood/storage-state.json",
        },
        () => ({
          origins: [{ origin: "https://example.com" }],
        }),
      ),
    /managed UI-test storage state is unreadable/,
  );
  assert.throws(
    () =>
      resolvePlaywrightBaseUrl(
        {
          MULTICA_UI_TEST_STORAGE_STATE:
            ".multica/ui-test/dogfood/storage-state.json",
        },
        () => ({
          cookies: [{ domain: "example.com", name: "session", value: "secret" }],
          origins: [
            {
              origin: "http://127.0.0.1:3000",
              localStorage: [
                { name: "multica_token", value: "top-secret-token" },
                { name: "multica:chat:isOpen", value: "false" },
              ],
            },
          ],
        }),
      ),
    /managed UI-test storage state is unreadable/,
  );
  assert.throws(
    () =>
      resolvePlaywrightBaseUrl(
        {
          MULTICA_UI_TEST_STORAGE_STATE:
            ".multica/ui-test/dogfood/storage-state.json",
        },
        () => ({
          cookies: [],
          origins: [
            {
              origin: "http://127.0.0.1:3000",
              localStorage: [
                { name: "multica_token", value: "top-secret-token" },
                { name: "multica:chat:isOpen", value: "false" },
              ],
            },
            {
              origin: "https://example.com",
              localStorage: [
                { name: "multica_token", value: "top-secret-token" },
              ],
            },
          ],
        }),
      ),
    /managed UI-test storage state is unreadable/,
  );
});

test("success diagnostics never expose the token", () => {
  const token = "top-secret-token";
  const output = formatSetupSuccess({
    workspaceSlug: "ui-test-dogfood",
    token,
  });

  assert.equal(output, "UI test setup ready for workspace ui-test-dogfood");
  assert.equal(output.includes(token), false);
  assert.doesNotThrow(() => JSON.stringify({ output }));
  assert.equal(JSON.stringify({ output }).includes(token), false);
});

test("failure diagnostics never expose caught secrets", () => {
  const secret = "postgres://user:password@host/database";
  const output = formatSetupFailure(new Error(`request body included ${secret}`));

  assert.equal(output, "UI test setup failed");
  assert.equal(output.includes(secret), false);
});

test("storage state is written with mode 0600 even over an existing file", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "multica-ui-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const path = join(directory, "storage-state.json");
  await writeFile(path, "old", { mode: 0o644 });
  await chmod(path, 0o644);

  await writeStorageStateSecurely(path, { cookies: [], origins: [] });

  assert.equal((await stat(path)).mode & 0o777, 0o600);
  assert.deepEqual(JSON.parse(await readFile(path, "utf8")), {
    cookies: [],
    origins: [],
  });
});

test("storage state rejects a symlink destination without touching its target", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "multica-ui-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const target = join(directory, "target.json");
  const path = join(directory, "storage-state.json");
  await writeFile(target, "do-not-touch");
  await symlink(target, path);

  await assert.rejects(
    writeStorageStateSecurely(path, { cookies: [], origins: [] }),
    /storage-state write failed/,
  );
  assert.equal(await readFile(target, "utf8"), "do-not-touch");
});
