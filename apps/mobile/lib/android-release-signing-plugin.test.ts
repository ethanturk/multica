import { describe, expect, it } from "vitest";

import {
  RELEASE_SIGNING_APPLY,
  RELEASE_SIGNING_GRADLE,
  ensureReleaseSigningApply,
} from "../plugins/with-android-release-signing";

describe("Android release signing config plugin", () => {
  it("appends one release-signing overlay to a Groovy app build file", () => {
    const initialBuildGradle = "plugins {\n  id 'com.android.application'\n}\n";

    const once = ensureReleaseSigningApply(initialBuildGradle);
    const twice = ensureReleaseSigningApply(once);

    expect(once).toBe(`${initialBuildGradle}\n${RELEASE_SIGNING_APPLY}\n`);
    expect(twice).toBe(once);
  });

  it("generates a fail-closed release signing configuration", () => {
    for (const environmentName of [
      "MULTICA_ANDROID_KEYSTORE_PATH",
      "MULTICA_ANDROID_KEYSTORE_PASSWORD",
      "MULTICA_ANDROID_KEY_ALIAS",
      "MULTICA_ANDROID_KEY_PASSWORD",
      "MULTICA_ANDROID_RELEASE_SIGNING_REQUIRED",
    ]) {
      expect(RELEASE_SIGNING_GRADLE).toContain(environmentName);
    }

    expect(RELEASE_SIGNING_GRADLE).toContain(
      "Missing required Android release signing environment variables:",
    );
    expect(RELEASE_SIGNING_GRADLE).toContain(
      "Android release keystore does not exist:",
    );
    expect(RELEASE_SIGNING_GRADLE).toContain("multicaRelease");
    expect(RELEASE_SIGNING_GRADLE).toContain(
      "signingConfig signingConfigs.multicaRelease",
    );
  });
});
