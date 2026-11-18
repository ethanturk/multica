import { describe, expect, it } from "vitest";

import {
  mcpSupportKind,
  providerSupportsMcpConfig,
  toolPlaneSupported,
} from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("accepts a provider whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
    expect(providerSupportsMcpConfig("devin")).toBe(true);
    expect(providerSupportsMcpConfig("zcode")).toBe(true);
    expect(providerSupportsMcpConfig("dirge")).toBe(true);
  });
  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig("droid")).toBe(false);
    expect(providerSupportsMcpConfig("amp")).toBe(false);
    expect(providerSupportsMcpConfig("pi")).toBe(true);
    // ZeroClaw's ACP server never reads `params.mcpServers` — MCP lives in
    // ZeroClaw's own config-dir, so a value saved here could not be honoured.
    expect(providerSupportsMcpConfig("zeroclaw")).toBe(false);
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

  // Pi gained native mcp_config support upstream, so it no longer needs the
  // pi-mcp-adapter path; native wins over the adapter set.
  it("classifies pi as native", () => {
    expect(mcpSupportKind("pi")).toBe("native");
  });
});

describe("toolPlaneSupported", () => {
  it("covers native and adapter-backed providers", () => {
    expect(toolPlaneSupported("codex")).toBe(true);
    expect(toolPlaneSupported("pi")).toBe(true);
    expect(toolPlaneSupported("gemini")).toBe(false);
  });
});
