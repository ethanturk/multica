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
// The run cancel button, inbox and timeline list have headless
// callback/prop tests with native modules mocked; these do not verify
// native rendering.
// App configuration tests also run here to cover native prebuild options
// across environment variants without loading React Native.
export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(appRoot),
    },
  },
  // React Native's bundler defines __DEV__; Expo modules read it at import
  // time, so component and route tests need it in Node too.
  define: {
    __DEV__: "false",
  },
  test: {
    environment: "node",
    globals: true,
    include: [
      "app.config.test.ts",
      "lib/**/*.test.ts",
      "data/**/*.test.ts",
      "data/**/*.test.tsx",
      "components/**/*.test.ts",
      "components/**/*.test.tsx",
      "route-tests/**/*.test.tsx",
    ],
    setupFiles: ["./vitest.setup.ts"],
    passWithNoTests: true,
  },
});
