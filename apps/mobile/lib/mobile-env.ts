const API_URL_MISSING_MESSAGE =
  "EXPO_PUBLIC_API_URL is not set. Add it to apps/mobile/.env.development.local " +
  "(see apps/mobile/.env.example for valid local/staging/production formats).";

export type MobileClientOs = "android" | "ios" | "web";

interface MobileEnvOptions {
  apiUrl?: string | null;
  platformOs?: string | null;
}

interface MobileEnv {
  apiUrl: string;
  wsUrl: string;
  clientOs: MobileClientOs;
}

function normalizeApiUrl(apiUrl: string | null | undefined): string {
  if (!apiUrl) {
    throw new Error(API_URL_MISSING_MESSAGE);
  }

  return apiUrl.replace(/\/+$/, "");
}

function toWebSocketUrl(apiUrl: string): string {
  if (apiUrl.startsWith("https://")) {
    return `wss://${apiUrl.slice("https://".length)}/ws`;
  }

  if (apiUrl.startsWith("http://")) {
    return `ws://${apiUrl.slice("http://".length)}/ws`;
  }

  throw new Error("EXPO_PUBLIC_API_URL must start with http:// or https://");
}

function resolveClientOs(platformOs: string | null | undefined): MobileClientOs {
  if (platformOs === "android") {
    return "android";
  }

  if (platformOs === "web") {
    return "web";
  }

  return "ios";
}

export function getMobileEnv(options: MobileEnvOptions = {}): MobileEnv {
  const apiUrl = normalizeApiUrl(
    options.apiUrl ?? process.env.EXPO_PUBLIC_API_URL ?? null,
  );

  return {
    apiUrl,
    wsUrl: toWebSocketUrl(apiUrl),
    clientOs: resolveClientOs(options.platformOs),
  };
}
