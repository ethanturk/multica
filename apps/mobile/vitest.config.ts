import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const appRoot = dirname(fileURLToPath(import.meta.url));

// Mobile Vitest runs in Node. It covers helpers, headless data-layer logic,
// and component/app tests with native dependencies mocked. We don't ship
// jsdom because the app runs on Hermes/native shims. Data-layer tests must
// mock `@/data/api` so the native fetch chain never loads.
//
// Co-located test files (foo.ts + foo.test.ts) match how the rest of the
// monorepo organises vitest suites.
export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(appRoot),
    },
  },
  test: {
    environment: "node",
    globals: true,
    include: [
      "lib/**/*.test.ts",
      "data/**/*.test.ts",
      "data/**/*.test.tsx",
      "components/**/*.test.tsx",
      "app/**/*.test.tsx",
    ],
    passWithNoTests: true,
  },
});
