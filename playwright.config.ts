import "./e2e/env";
import { readFileSync } from "node:fs";
import { defineConfig } from "@playwright/test";
import { resolvePlaywrightBaseUrl } from "./e2e/ui-test-setup-lib.mjs";

export default defineConfig({
  testDir: "./e2e",
  // The performance scenario has its own config: one worker, no retries, and a
  // running production build. It must not be swept up by the ordinary suite.
  testIgnore: "**/perf/**",
  timeout: 60000,
  workers: 1,
  retries: 0,
  use: {
    baseURL: resolvePlaywrightBaseUrl(process.env, (path: string) =>
      JSON.parse(readFileSync(path, "utf8")),
    ),
    headless: true,
    storageState: process.env.MULTICA_UI_TEST_STORAGE_STATE || undefined,
  },
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
  ],
  // Don't auto-start servers — they must be running already
  // This avoids complexity and port conflicts during testing
});
