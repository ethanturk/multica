import { describe, expect, it } from "vitest";
import { resolvePlatformSymbol } from "./platform-symbols";

describe("resolvePlatformSymbol", () => {
  it("maps tab bar symbols to Android-safe Ionicons", () => {
    expect(resolvePlatformSymbol("tray")).toBe("file-tray-outline");
    expect(resolvePlatformSymbol("tray.fill")).toBe("file-tray");
    expect(resolvePlatformSymbol("bubble.left.fill")).toBe("chatbubble");
  });

  it("maps disclosure and selection symbols", () => {
    expect(resolvePlatformSymbol("chevron.right")).toBe("chevron-forward");
    expect(resolvePlatformSymbol("checkmark")).toBe("checkmark");
  });
});
