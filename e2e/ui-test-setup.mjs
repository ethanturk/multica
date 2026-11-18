import { existsSync } from "node:fs";
import { resolve } from "node:path";

import { config as loadEnv } from "dotenv";
import pg from "pg";

import {
  buildStorageState,
  deriveUiTestIdentity,
  formatSetupFailure,
  formatSetupSuccess,
  readSetupConfig,
  writeStorageStateSecurely,
} from "./ui-test-setup-lib.mjs";

for (const filename of [".env.worktree", ".env"]) {
  const path = resolve(process.cwd(), filename);
  if (existsSync(path)) {
    loadEnv({ path, quiet: true });
    break;
  }
}

const apiBase =
  process.env.NEXT_PUBLIC_API_URL ||
  `http://localhost:${process.env.PORT || "8080"}`;
const databaseUrl =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

async function jsonRequest(url, init, label) {
  const response = await fetch(url, init);
  if (!response.ok) {
    throw new Error(`${label} failed with HTTP ${response.status}`);
  }
  return response.json();
}

async function authenticatedRequest(path, token, init = {}) {
  return fetch(`${apiBase}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      ...init.headers,
    },
  });
}

async function ensureWorkspace(token, identity) {
  const listResponse = await authenticatedRequest("/api/workspaces", token);
  if (!listResponse.ok) {
    throw new Error(`workspace lookup failed with HTTP ${listResponse.status}`);
  }
  const workspaces = await listResponse.json();
  const existing = workspaces.find(
    (workspace) => workspace.slug === identity.workspaceSlug,
  );
  if (existing) return existing;

  const createResponse = await authenticatedRequest("/api/workspaces", token, {
    method: "POST",
    body: JSON.stringify({
      name: identity.workspaceName,
      slug: identity.workspaceSlug,
    }),
  });
  if (createResponse.ok) return createResponse.json();
  if (createResponse.status !== 409) {
    throw new Error(
      `workspace creation failed with HTTP ${createResponse.status}`,
    );
  }

  const refreshedResponse = await authenticatedRequest("/api/workspaces", token);
  if (!refreshedResponse.ok) {
    throw new Error(
      `workspace lookup failed with HTTP ${refreshedResponse.status}`,
    );
  }
  const refreshed = await refreshedResponse.json();
  const racedWorkspace = refreshed.find(
    (workspace) => workspace.slug === identity.workspaceSlug,
  );
  if (!racedWorkspace) throw new Error("workspace creation did not converge");
  return racedWorkspace;
}

async function setup() {
  const setupConfig = readSetupConfig(process.env);
  const identity = deriveUiTestIdentity(setupConfig.taskId);
  const client = new pg.Client(databaseUrl);
  let connected = false;

  try {
    await client.connect();
    connected = true;
    await client.query("DELETE FROM verification_code WHERE email = $1", [
      identity.email,
    ]);

    await jsonRequest(
      `${apiBase}/auth/send-code`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: identity.email }),
      },
      "send-code",
    );

    const codeResult = await client.query(
      "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
      [identity.email],
    );
    const databaseCode = codeResult.rows[0]?.code;
    if (!databaseCode) throw new Error("verification code was not created");
    const code =
      process.env.MULTICA_DEV_VERIFICATION_CODE?.trim() || databaseCode;

    const login = await jsonRequest(
      `${apiBase}/auth/verify-code`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: identity.email, code }),
      },
      "verify-code",
    );
    if (typeof login.token !== "string" || !login.token) {
      throw new Error("verify-code response did not contain a token");
    }

    const workspace = await ensureWorkspace(login.token, identity);
    if (workspace.slug !== identity.workspaceSlug) {
      throw new Error("workspace response did not match requested slug");
    }

    const onboarded = await client.query(
      `
        UPDATE "user"
        SET
          onboarded_at = COALESCE(onboarded_at, now()),
          onboarding_questionnaire = COALESCE(onboarding_questionnaire, '{}'::jsonb)
            || '{"source":["friends_colleagues"],"source_other":null,"source_skipped":false}'::jsonb
        WHERE email = $1
      `,
      [identity.email],
    );
    if (onboarded.rowCount !== 1) {
      throw new Error("test user onboarding update failed");
    }

    const storageState = buildStorageState({
      baseUrl: setupConfig.baseUrl,
      token: login.token,
    });
    await writeStorageStateSecurely(
      setupConfig.storageStatePath,
      storageState,
    );
    console.log(
      formatSetupSuccess({
        workspaceSlug: workspace.slug,
        token: login.token,
      }),
    );
  } finally {
    if (connected) {
      try {
        await client.query("DELETE FROM verification_code WHERE email = $1", [
          identity.email,
        ]);
      } finally {
        await client.end();
      }
    }
  }
}

try {
  await setup();
} catch {
  console.error(formatSetupFailure());
  process.exitCode = 1;
}
