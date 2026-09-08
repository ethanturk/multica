import { describe, expect, it } from "vitest";

import {
  mcpSupportKind,
  providerSupportsMcpConfig,
  toolPlaneSupported,
} from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("accepts a provider whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
    expect(providerSupportsMcpConfig("dirge")).toBe(true);
  });
  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("antigravity")).toBe(false);
    expect(providerSupportsMcpConfig("copilot")).toBe(false);
    expect(providerSupportsMcpConfig("gemini")).toBe(false);
    // Pi ships without MCP by design: upstream's README states "No MCP." and
    // directs users to extensions instead, so there is no config file Multica
    // could write that pi would read. Only its omp fork consumes mcp_config.
    expect(providerSupportsMcpConfig("pi")).toBe(false);
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
