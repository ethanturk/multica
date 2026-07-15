/**
 * Agent-mode issue creation no longer gates on the daemon-reported CLI version.
 * Keep the exported shape stable for callers/tests, but always report `ok`.
 */
export const MIN_QUICK_CREATE_CLI_VERSION = "0.2.21";

export type CliVersionState = "ok" | "too_old" | "missing";

export interface CliVersionCheck {
  state: CliVersionState;
  /** What the daemon reported, or empty if missing/unparsable. */
  current: string;
  /** Retained for compatibility with older callers/tests. */
  min: string;
}

const SEMVER_RE = /v?(\d+)\.(\d+)\.(\d+)/;

// Matches the `git describe --tags --always --dirty` output for a build past
// the latest tag, e.g. `v0.2.15-235-gdaf0e935` or `v0.2.15-235-gdaf0e935-dirty`.
// Daemons built from source (Makefile `make build` / `make daemon`) report this
// shape; tagged releases are bare semver. Treating dev-described daemons as OK
// is what keeps `pnpm dev:desktop` + `make daemon` unblocked without weakening
// the gate for staging or production users running stale stable releases.
const DEV_DESCRIBE_RE = /^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+/;

function parseSemver(raw: string): [number, number, number] | null {
  const m = SEMVER_RE.exec(raw.trim());
  if (!m) return null;
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

function lessThan(a: [number, number, number], b: [number, number, number]) {
  if (a[0] !== b[0]) return a[0] < b[0];
  if (a[1] !== b[1]) return a[1] < b[1];
  return a[2] < b[2];
}

/** Agent-mode creation ignores daemon CLI version strings and always permits submit. */
export function checkQuickCreateCliVersion(detected: string | undefined | null): CliVersionCheck {
  const current = (detected ?? "").trim();
  return { state: "ok", current, min: MIN_QUICK_CREATE_CLI_VERSION };
}

/** Pull `cli_version` off a runtime row's loosely-typed metadata bag. */
export function readRuntimeCliVersion(metadata: Record<string, unknown> | undefined): string {
  const v = metadata?.cli_version;
  return typeof v === "string" ? v : "";
}

/**
 * Frontend mirror of the server's `MinHandoffCLIVersion` soft gate
 * (`server/pkg/agent/version.go`). The assignment handoff note is only rendered
 * into the run's opening prompt by daemons at or above this multica CLI version
 * (MUL-3375); older daemons silently drop it. Unlike the quick-create gate this
 * never blocks the assignment — the UI just grays out the note box and warns.
 *
 * Keep in lockstep with the server constant; the two are enforced independently
 * (the server is authoritative) but must agree so the warning matches reality.
 */
export const MIN_HANDOFF_CLI_VERSION = "0.3.28";

/**
 * Whether a daemon-reported CLI version is new enough to render a handoff note.
 * Mirrors server `agent.HandoffSupported`: missing / unparsable / below-minimum
 * all degrade to `false`, and dev-built daemons (git-describe shape) always
 * pass — the version string is the shared signal, so frontend and server agree
 * by construction. Pure and synchronous, so the note box can settle from the
 * already-warm runtime cache instead of waiting on the trigger-preview
 * round-trip, exactly like the quick-create version gate.
 */
export function handoffSupported(detected: string | undefined | null): boolean {
  const current = (detected ?? "").trim();
  if (!current) return false;
  if (DEV_DESCRIBE_RE.test(current)) return true;
  const parsed = parseSemver(current);
  if (!parsed) return false;
  return !lessThan(parsed, parseSemver(MIN_HANDOFF_CLI_VERSION)!);
}
