import { afterEach, describe, expect, it } from "vitest";

import { getMobileEnv } from "./mobile-env";

describe("getMobileEnv", () => {
  const originalApiUrl = process.env.EXPO_PUBLIC_API_URL;

  afterEach(() => {
    if (originalApiUrl === undefined) {
      delete process.env.EXPO_PUBLIC_API_URL;
    } else {
      process.env.EXPO_PUBLIC_API_URL = originalApiUrl;
    }
  });

  it("returns ios defaults from the configured API URL", () => {
    expect(
      getMobileEnv({
        apiUrl: "https://api.multica.ai",
      }),
    ).toEqual({
      apiUrl: "https://api.multica.ai",
      wsUrl: "wss://api.multica.ai/ws",
      clientOs: "ios",
    });
  });

  it("returns android metadata and trims a trailing slash", () => {
    expect(
      getMobileEnv({
        apiUrl: "https://staging.multica.ai/",
        platformOs: "android",
      }),
    ).toEqual({
      apiUrl: "https://staging.multica.ai",
      wsUrl: "wss://staging.multica.ai/ws",
      clientOs: "android",
    });
  });

  it("derives an insecure websocket URL for local development", () => {
    expect(
      getMobileEnv({
        apiUrl: "http://10.0.2.2:8080",
        platformOs: "android",
      }),
    ).toEqual({
      apiUrl: "http://10.0.2.2:8080",
      wsUrl: "ws://10.0.2.2:8080/ws",
      clientOs: "android",
    });
  });

  it("returns web when requested explicitly", () => {
    expect(
      getMobileEnv({
        apiUrl: "https://api.multica.ai",
        platformOs: "web",
      }),
    ).toMatchObject({
      clientOs: "web",
    });
  });

  it("reads the API URL from the Expo public environment by default", () => {
    process.env.EXPO_PUBLIC_API_URL = "https://prod.multica.ai///";

    expect(getMobileEnv()).toEqual({
      apiUrl: "https://prod.multica.ai",
      wsUrl: "wss://prod.multica.ai/ws",
      clientOs: "ios",
    });
  });

  it("falls back to ios when the platform is unknown", () => {
    expect(
      getMobileEnv({
        apiUrl: "https://api.multica.ai",
        platformOs: "windows",
      }),
    ).toMatchObject({
      clientOs: "ios",
    });
  });

  it("throws when the API URL is missing", () => {
    expect(() => getMobileEnv({ apiUrl: undefined })).toThrow(
      "EXPO_PUBLIC_API_URL is not set.",
    );
  });

  it("throws when the API URL is not http or https", () => {
    expect(() =>
      getMobileEnv({
        apiUrl: "ftp://api.multica.ai",
      }),
    ).toThrow("EXPO_PUBLIC_API_URL must start with http:// or https://");
  });
});
