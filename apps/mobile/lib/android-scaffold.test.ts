import { existsSync, readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const readFixture = (relativePath: string) =>
  readFileSync(new URL(relativePath, import.meta.url), "utf8");

const mobilePackage = JSON.parse(
  readFixture("../package.json"),
) as {
  scripts: Record<string, string>;
};

const workspacePackage = JSON.parse(
  readFixture("../../../package.json"),
) as {
  scripts: Record<string, string>;
};

describe("android scaffold", () => {
  it("keeps the committed Android native project in the mobile package", () => {
    const requiredPaths = [
      "../android/build.gradle",
      "../android/settings.gradle",
      "../android/gradlew",
      "../android/app/build.gradle",
      "../android/app/src/main/AndroidManifest.xml",
      "../android/app/src/main/java/ai/multica/mobile/dev/MainActivity.kt",
      "../android/app/src/main/java/ai/multica/mobile/dev/MainApplication.kt",
      "../android/app/src/main/res/values/strings.xml",
    ];

    for (const relativePath of requiredPaths) {
      expect(existsSync(new URL(relativePath, import.meta.url))).toBe(true);
    }
  });

  it("wires the Android native config to the dev package identity and deep links", () => {
    const buildGradle = readFixture("../android/app/build.gradle");
    const manifest = readFixture("../android/app/src/main/AndroidManifest.xml");
    const settingsGradle = readFixture("../android/settings.gradle");

    expect(buildGradle).toContain("namespace 'ai.multica.mobile.dev'");
    expect(buildGradle).toContain("applicationId 'ai.multica.mobile.dev'");
    expect(manifest).toContain('android:name=".MainApplication"');
    expect(manifest).toContain('android:name=".MainActivity"');
    expect(manifest).toContain('<data android:scheme="multica"/>');
    expect(manifest).toContain('<data android:scheme="exp+multica-mobile"/>');
    expect(settingsGradle).toContain("rootProject.name = 'Multica (Dev)'");
  });

  it("exposes Android run scripts for local, staging, and production variants", () => {
    expect(mobilePackage.scripts).toMatchObject({
      android: "expo run:android",
      "android:staging":
        "dotenv -e .env.staging -- cross-env APP_ENV=staging expo run:android",
      "android:prod":
        "dotenv -e .env.production -- cross-env APP_ENV=production expo run:android",
    });

    expect(workspacePackage.scripts).toMatchObject({
      "android:mobile": "pnpm -C apps/mobile android",
      "android:mobile:staging": "pnpm -C apps/mobile android:staging",
      "android:mobile:prod": "pnpm -C apps/mobile android:prod",
    });
  });

  it("documents Android local-backend setup in both env templates and the README", () => {
    const envExample = readFixture("../.env.example");
    const readme = readFixture("../README.md");

    expect(envExample).toContain("Android Emulator: use `http://10.0.2.2:8080`");
    expect(envExample).toContain("Physical iPhone / Android device: use your Mac's LAN IP");
    expect(readme).toContain("## Run it on Android");
    expect(readme).toContain("pnpm android:mobile:staging");
    expect(readme).toContain("For a local backend, set `EXPO_PUBLIC_API_URL=http://10.0.2.2:8080`");
    expect(readme).toContain("point `EXPO_PUBLIC_API_URL` at your Mac's LAN IP");
  });

  it("leaves iOS as a shared-config concern instead of committing a native ios project", () => {
    expect(existsSync(new URL("../ios", import.meta.url))).toBe(false);
  });
});
