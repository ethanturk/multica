import { describe, expect, it, vi } from "vitest";

import {
  buildUITestInstallArgs,
  buildUITestStatusArgs,
  createUITestCapabilityOperations,
  parseUITestCapabilityJSON,
  parseUITestCapabilityStatus,
} from "./ui-test-status";
import {
  getUITestCapabilityPresentation,
  getUITestCapabilityAction,
} from "../shared/daemon-types";

describe("parseUITestCapabilityStatus", () => {
  it.each([
    [{ status: "unavailable" }, { status: "unavailable" }],
    [{ status: "installing" }, { status: "installing" }],
    [
      { status: "ready", version: "0.0.78" },
      { status: "ready", version: "0.0.78" },
    ],
    [
      { status: "broken", error: "runtime missing" },
      { status: "broken", error: "runtime missing" },
    ],
  ])("accepts a valid capability status", (input, expected) => {
    expect(parseUITestCapabilityStatus(input)).toEqual(expected);
  });

  it.each([
    null,
    [],
    {},
    { status: "unknown" },
    { status: "ready" },
    { status: "ready", version: 78 },
    { status: "broken", error: false },
    { status: "ready", version: "0.0.78", extra: true },
  ])("fails closed on malformed status %#", (input) => {
    expect(parseUITestCapabilityStatus(input)).toEqual({
      status: "broken",
      error: "Invalid UI testing status.",
    });
  });

  it("fails closed on malformed CLI JSON", () => {
    expect(parseUITestCapabilityJSON("{not-json")).toEqual({
      status: "broken",
      error: "Invalid UI testing status.",
    });
  });

  it("keeps older daemon health compatible when ui_test is omitted", () => {
    expect(
      parseUITestCapabilityStatus(undefined, { optional: true }),
    ).toBeUndefined();
  });

  it("bounds daemon errors before they reach the settings row", () => {
    const parsed = parseUITestCapabilityStatus({
      status: "broken",
      error: "x".repeat(500),
    });

    expect(parsed.error).toHaveLength(160);
  });
});

describe("UI test status presentation", () => {
  it.each([
    ["unavailable", "Not installed", "Install"],
    ["installing", "Installing…", "Installing…"],
    ["ready", "Ready", null],
    ["broken", "Needs repair", "Repair"],
  ] as const)("covers %s", (status, label, actionLabel) => {
    const capability =
      status === "ready"
        ? { status, version: "0.0.78" }
        : status === "broken"
          ? { status, error: "runtime missing" }
          : { status };
    const presentation = getUITestCapabilityPresentation(capability);

    expect(presentation.label).toBe(label);
    expect(presentation.description.length).toBeGreaterThan(0);
    expect(getUITestCapabilityAction(capability, true)?.label ?? null).toBe(
      actionLabel,
    );
  });

  it("disables install and repair when the CLI is unavailable", () => {
    expect(
      getUITestCapabilityAction({ status: "unavailable" }, false),
    ).toEqual({ label: "Install", disabled: true });
    expect(
      getUITestCapabilityAction(
        { status: "broken", error: "runtime missing" },
        false,
      ),
    ).toEqual({ label: "Repair", disabled: true });
    expect(
      getUITestCapabilityAction({ status: "installing" }, true),
    ).toEqual({ label: "Installing…", disabled: true });
  });
});

describe("UI test CLI arguments", () => {
  it("builds exact default-profile arguments", () => {
    expect(buildUITestStatusArgs("")).toEqual([
      "ui-test",
      "status",
      "--output",
      "json",
    ]);
    expect(buildUITestInstallArgs("")).toEqual(["ui-test", "install"]);
  });

  it("builds exact named-profile arguments", () => {
    expect(buildUITestStatusArgs("desktop-localhost-3000")).toEqual([
      "--profile",
      "desktop-localhost-3000",
      "ui-test",
      "status",
      "--output",
      "json",
    ]);
    expect(buildUITestInstallArgs("desktop-localhost-3000")).toEqual([
      "--profile",
      "desktop-localhost-3000",
      "ui-test",
      "install",
    ]);
  });
});

describe("createUITestCapabilityOperations", () => {
  it("coalesces concurrent installs and returns final parsed status", async () => {
    let releaseInstall!: () => void;
    const installBlocked = new Promise<void>((resolve) => {
      releaseInstall = resolve;
    });
    const run = vi.fn(async (args: readonly string[]) => {
      if (args.includes("install")) {
        await installBlocked;
        return "";
      }
      return '{"status":"ready","version":"0.0.78"}';
    });
    const operations = createUITestCapabilityOperations(
      run,
      () => "desktop-local",
    );

    const first = operations.install();
    const second = operations.install();
    expect(first).toBe(second);
    await Promise.resolve();
    expect(run).toHaveBeenCalledTimes(1);
    releaseInstall();

    await expect(first).resolves.toEqual({
      status: "ready",
      version: "0.0.78",
    });
    await expect(second).resolves.toEqual({
      status: "ready",
      version: "0.0.78",
    });
    expect(run).toHaveBeenCalledTimes(2);
  });

  it("returns a concise broken status when CLI execution fails", async () => {
    const operations = createUITestCapabilityOperations(
      async () => {
        throw new Error("secret-bearing stderr");
      },
      () => "",
    );

    await expect(operations.status()).resolves.toEqual({
      status: "broken",
      error: "UI testing status check failed.",
    });
    await expect(operations.install()).resolves.toEqual({
      status: "broken",
      error: "UI testing installation failed.",
    });
  });
});
