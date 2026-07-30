import type { UITestCapabilityStatus } from "../shared/daemon-types";

const INVALID_STATUS: UITestCapabilityStatus = {
  status: "broken",
  error: "Invalid UI testing status.",
};
const STATES = new Set(["unavailable", "installing", "ready", "broken"]);
const STATUS_KEYS = new Set(["status", "version", "error"]);
const MAX_ERROR_LENGTH = 160;

export function parseUITestCapabilityStatus(
  value: unknown,
): UITestCapabilityStatus;
export function parseUITestCapabilityStatus(
  value: unknown,
  options: { optional: true },
): UITestCapabilityStatus | undefined;
export function parseUITestCapabilityStatus(
  value: unknown,
  options?: { optional: true },
): UITestCapabilityStatus | undefined {
  if (value === undefined && options?.optional) return undefined;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return INVALID_STATUS;
  }

  const candidate = value as Record<string, unknown>;
  if (
    Object.keys(candidate).some((key) => !STATUS_KEYS.has(key)) ||
    typeof candidate.status !== "string" ||
    !STATES.has(candidate.status) ||
    (candidate.version !== undefined &&
      typeof candidate.version !== "string") ||
    (candidate.error !== undefined && typeof candidate.error !== "string")
  ) {
    return INVALID_STATUS;
  }

  const version = candidate.version?.trim();
  if (candidate.status === "ready" && !version) return INVALID_STATUS;
  const error = candidate.error?.trim().slice(0, MAX_ERROR_LENGTH);

  return {
    status: candidate.status as UITestCapabilityStatus["status"],
    ...(version ? { version } : {}),
    ...(error ? { error } : {}),
  };
}

export function parseUITestCapabilityJSON(
  output: string,
): UITestCapabilityStatus {
  try {
    return parseUITestCapabilityStatus(JSON.parse(output));
  } catch {
    return INVALID_STATUS;
  }
}

function profileArgs(profile: string): string[] {
  return profile ? ["--profile", profile] : [];
}

export function buildUITestStatusArgs(profile: string): string[] {
  return [
    ...profileArgs(profile),
    "ui-test",
    "status",
    "--output",
    "json",
  ];
}

export function buildUITestInstallArgs(profile: string): string[] {
  return [...profileArgs(profile), "ui-test", "install"];
}

type RunCLI = (args: readonly string[]) => Promise<string>;
type GetProfile = () => string | Promise<string>;

export function createUITestCapabilityOperations(
  run: RunCLI,
  getProfile: GetProfile,
): {
  status(): Promise<UITestCapabilityStatus>;
  install(): Promise<UITestCapabilityStatus>;
} {
  let installPromise: Promise<UITestCapabilityStatus> | null = null;

  const status = async (): Promise<UITestCapabilityStatus> => {
    try {
      return parseUITestCapabilityJSON(
        await run(buildUITestStatusArgs(await getProfile())),
      );
    } catch {
      return {
        status: "broken",
        error: "UI testing status check failed.",
      };
    }
  };

  const install = (): Promise<UITestCapabilityStatus> => {
    if (installPromise) return installPromise;
    installPromise = (async () => {
      try {
        await run(buildUITestInstallArgs(await getProfile()));
        return await status();
      } catch {
        return {
          status: "broken",
          error: "UI testing installation failed.",
        } as const;
      }
    })().finally(() => {
      installPromise = null;
    });
    return installPromise;
  };

  return { status, install };
}
