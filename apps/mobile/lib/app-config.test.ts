import type { ConfigContext } from "expo/config";
import { afterEach, describe, expect, it } from "vitest";

import mobileConfigFactory from "../app.config";

const CONFIG_CONTEXT = {
  config: {} as never,
  packageJsonPath: "",
  projectRoot: "",
  staticConfigPath: null,
} satisfies ConfigContext;

describe("mobile app config", () => {
  const originalAppEnv = process.env.APP_ENV;
  const originalAndroidVersionCode = process.env.ANDROID_VERSION_CODE;
  const originalBundleIdentifierDev = process.env.EXPO_BUNDLE_IDENTIFIER_DEV;
  const originalBundleIdentifierProd = process.env.EXPO_BUNDLE_IDENTIFIER_PROD;

  afterEach(() => {
    if (originalAppEnv === undefined) {
      delete process.env.APP_ENV;
    } else {
      process.env.APP_ENV = originalAppEnv;
    }

    if (originalAndroidVersionCode === undefined) {
      delete process.env.ANDROID_VERSION_CODE;
    } else {
      process.env.ANDROID_VERSION_CODE = originalAndroidVersionCode;
    }

    if (originalBundleIdentifierDev === undefined) {
      delete process.env.EXPO_BUNDLE_IDENTIFIER_DEV;
    } else {
      process.env.EXPO_BUNDLE_IDENTIFIER_DEV = originalBundleIdentifierDev;
    }

    if (originalBundleIdentifierProd === undefined) {
      delete process.env.EXPO_BUNDLE_IDENTIFIER_PROD;
    } else {
      process.env.EXPO_BUNDLE_IDENTIFIER_PROD = originalBundleIdentifierProd;
    }
  });

  it("returns the development package identifiers by default", () => {
    delete process.env.APP_ENV;
    delete process.env.ANDROID_VERSION_CODE;
    delete process.env.EXPO_BUNDLE_IDENTIFIER_DEV;

    const config = mobileConfigFactory(CONFIG_CONTEXT);

    expect(config.name).toBe("Multica (Dev)");
    expect(config.android?.package).toBe("ai.multica.mobile.dev");
    expect(config.android?.versionCode).toBe(1);
    expect(config.ios?.bundleIdentifier).toBe("ai.multica.mobile.dev");
    expect(config.android?.adaptiveIcon).toEqual({
      foregroundImage: "./assets/icon.png",
      backgroundColor: "#090909",
    });
  });

  it("returns staging identifiers when APP_ENV is staging", () => {
    process.env.APP_ENV = "staging";

    const config = mobileConfigFactory(CONFIG_CONTEXT);

    expect(config.name).toBe("Multica (Staging)");
    expect(config.android?.package).toBe("ai.multica.mobile.staging");
    expect(config.ios?.bundleIdentifier).toBe("ai.multica.mobile.staging");
  });

  it("returns production identifiers and honors the prod ios override", () => {
    process.env.APP_ENV = "production";
    process.env.EXPO_BUNDLE_IDENTIFIER_PROD = "com.example.multica";

    const config = mobileConfigFactory(CONFIG_CONTEXT);

    expect(config.name).toBe("Multica");
    expect(config.android?.package).toBe("ai.multica.mobile");
    expect(config.ios?.bundleIdentifier).toBe("com.example.multica");
  });

  it("honors the dev ios override without changing the android package", () => {
    process.env.APP_ENV = "development";
    process.env.EXPO_BUNDLE_IDENTIFIER_DEV = "com.example.multica.dev";

    const config = mobileConfigFactory(CONFIG_CONTEXT);

    expect(config.android?.package).toBe("ai.multica.mobile.dev");
    expect(config.ios?.bundleIdentifier).toBe("com.example.multica.dev");
  });

  it("uses a supplied positive Android version code", () => {
    process.env.ANDROID_VERSION_CODE = "42";

    const config = mobileConfigFactory(CONFIG_CONTEXT);

    expect(config.android?.versionCode).toBe(42);
  });

  it.each(["0", "-1", "1.5", "abc"])(
    "rejects invalid Android version code %s",
    (versionCode) => {
      process.env.ANDROID_VERSION_CODE = versionCode;

      expect(() => mobileConfigFactory(CONFIG_CONTEXT)).toThrow(
        "ANDROID_VERSION_CODE must be a positive integer",
      );
    },
  );
});
