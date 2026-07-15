import { describe, expect, it } from "vitest";

import {
  mcpSupportKind,
  providerSupportsMcpConfig,
  toolPlaneSupported,
} from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("matches providers whose runtime consumes mcp_config", () => {
for (const p of [
      "claude",
      "codebuddy",
      "codex",
      "cursor",
      "dirge",
      "hermes",
      "kimi",
      "kiro",
      "opencode",
      "openclaw",
      "qoder",
      "traecli",
    ]) {
      expect(providerSupportsMcpConfig(p)).toBe(true);
    }
expect(providerSupportsMcpConfig("claude")).toBe(true);
    expect(providerSupportsMcpConfig("codebuddy")).toBe(true);
    expect(providerSupportsMcpConfig("codex")).toBe(true);
    expect(providerSupportsMcpConfig("cursor")).toBe(true);
    expect(providerSupportsMcpConfig("hermes")).toBe(true);
    expect(providerSupportsMcpConfig("kimi")).toBe(true);
    expect(providerSupportsMcpConfig("kiro")).toBe(true);
    expect(providerSupportsMcpConfig("opencode")).toBe(true);
    expect(providerSupportsMcpConfig("openclaw")).toBe(true);
    expect(providerSupportsMcpConfig("qoder")).toBe(true);
    expect(providerSupportsMcpConfig("traecli")).toBe(true);
    expect(providerSupportsMcpConfig("grok")).toBe(true);
  });

  it("rejects providers whose runtime ignores mcp_config and nullish input", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig("gemini")).toBe(false);
    // pi is adapter-backed, not native, so the native mcp_config tab stays hidden.
    expect(providerSupportsMcpConfig("pi")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});

describe("mcpSupportKind", () => {
  it("classifies native providers", () => {
    expect(mcpSupportKind("claude")).toBe("native");
    expect(mcpSupportKind("dirge")).toBe("native");
    expect(mcpSupportKind("openclaw")).toBe("native");
  });

  it("classifies unsupported providers as none", () => {
    expect(mcpSupportKind("gemini")).toBe("none");
    expect(mcpSupportKind(undefined)).toBe("none");
  });

  it("classifies pi as adapter-backed", () => {
    expect(mcpSupportKind("pi")).toBe("adapter");
  });
});

describe("toolPlaneSupported", () => {
  it("covers native and adapter-backed providers", () => {
    expect(toolPlaneSupported("codex")).toBe(true);
    expect(toolPlaneSupported("pi")).toBe(true);
    expect(toolPlaneSupported("gemini")).toBe(false);
  });
});
