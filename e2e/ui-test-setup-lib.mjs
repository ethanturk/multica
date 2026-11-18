import { chmod, lstat, mkdir, rename, rm, writeFile } from "node:fs/promises";
import { isIP } from "node:net";
import { dirname } from "node:path";

const SAFE_TASK_ID = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

function required(value, label) {
  const normalized = typeof value === "string" ? value.trim() : "";
  if (!normalized) throw new Error(`${label} is required`);
  return normalized;
}

function loopbackOrigin(raw) {
  let url;
  try {
    url = new URL(raw);
  } catch {
    throw new Error("invalid UI test URL");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("UI test URL must use http or https");
  }

  const hostname = url.hostname.toLowerCase().replace(/^\[|\]$/g, "");
  const ipVersion = isIP(hostname);
  const isLoopback =
    hostname === "localhost" ||
    (ipVersion === 4 && hostname.startsWith("127.")) ||
    (ipVersion === 6 && hostname === "::1");
  if (url.username || url.password || url.hash || !isLoopback) {
    throw new Error(
      "UI test URL must target loopback without credentials or fragments",
    );
  }
  return url.origin;
}

export function deriveUiTestIdentity(taskId) {
  const safeTaskId = required(taskId, "task ID")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!safeTaskId || !SAFE_TASK_ID.test(safeTaskId)) {
    throw new Error("task ID must contain a letter or digit");
  }

  const workspaceSlug = `ui-test-${safeTaskId}`;
  if (workspaceSlug.length > 63) {
    throw new Error("task ID is too long");
  }

  return {
    email: `${workspaceSlug}@multica.ai`,
    workspaceName: `UI Test ${safeTaskId}`,
    workspaceSlug,
  };
}

export function readSetupConfig(env) {
  return {
    taskId: required(env.MULTICA_UI_TEST_TASK_ID, "task ID"),
    baseUrl: required(env.MULTICA_UI_TEST_BASE_URL, "base URL"),
    storageStatePath: required(
      env.MULTICA_UI_TEST_STORAGE_STATE,
      "storage-state path",
    ),
  };
}

export function resolvePlaywrightBaseUrl(env, loadStorageState) {
  let storageOrigin;
  if (env.MULTICA_UI_TEST_STORAGE_STATE) {
    try {
      if (!loadStorageState) throw new Error();
      const state = loadStorageState(env.MULTICA_UI_TEST_STORAGE_STATE);
      if (
        !Array.isArray(state?.cookies) ||
        state.cookies.length !== 0 ||
        !Array.isArray(state.origins) ||
        state.origins.length !== 1
      ) {
        throw new Error();
      }
      const managedOrigin = state.origins[0];
      const storage = managedOrigin?.localStorage;
      if (!Array.isArray(storage) || storage.length !== 2) throw new Error();
      const token = storage.find((item) => item?.name === "multica_token");
      const chat = storage.find(
        (item) => item?.name === "multica:chat:isOpen",
      );
      if (
        typeof token?.value !== "string" ||
        !token.value ||
        chat?.value !== "false"
      ) {
        throw new Error();
      }
      storageOrigin = loopbackOrigin(managedOrigin.origin);
    } catch {
      throw new Error("managed UI-test storage state is unreadable");
    }
  }

  if (env.MULTICA_UI_TEST_BASE_URL) {
    const explicitOrigin = loopbackOrigin(env.MULTICA_UI_TEST_BASE_URL);
    if (storageOrigin && storageOrigin !== explicitOrigin) {
      throw new Error("managed UI-test origin does not match storage state");
    }
    return explicitOrigin;
  }
  if (storageOrigin) return storageOrigin;

  return (
    env.PLAYWRIGHT_BASE_URL ||
    env.FRONTEND_ORIGIN ||
    "http://localhost:3000"
  );
}

export function buildStorageState({ baseUrl, token }) {
  const safeToken = required(token, "token");
  const rawBaseUrl = required(baseUrl, "base URL");
  const origin = loopbackOrigin(rawBaseUrl);

  return {
    cookies: [],
    origins: [
      {
        origin,
        localStorage: [
          { name: "multica_token", value: safeToken },
          { name: "multica:chat:isOpen", value: "false" },
        ],
      },
    ],
  };
}

export function formatSetupSuccess({ workspaceSlug }) {
  return `UI test setup ready for workspace ${workspaceSlug}`;
}

export function formatSetupFailure() {
  return "UI test setup failed";
}

export async function writeStorageStateSecurely(path, state) {
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  const temporaryPath = `${path}.${process.pid}.tmp`;
  try {
    await writeFile(temporaryPath, `${JSON.stringify(state, null, 2)}\n`, {
      flag: "wx",
      mode: 0o600,
    });
    await chmod(temporaryPath, 0o600);
    try {
      const existing = await lstat(path);
      if (!existing.isFile()) throw new Error("unsafe storage-state target");
      await rm(path);
    } catch (error) {
      if (error?.code !== "ENOENT") throw error;
    }
    await rename(temporaryPath, path);
    await chmod(path, 0o600);
  } catch {
    await rm(temporaryPath, { force: true });
    throw new Error("storage-state write failed");
  }
}
