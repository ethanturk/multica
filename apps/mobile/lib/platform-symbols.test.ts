import { describe, expect, it } from "vitest";
import { resolvePlatformSymbol } from "./platform-symbols";

const symbolCases = [
  ["tray", "file-tray-outline"],
  ["tray.fill", "file-tray"],
  ["checklist", "checkmark-circle"],
  ["checklist.unchecked", "checkmark-circle-outline"],
  ["bubble.left", "chatbubble-outline"],
  ["bubble.left.fill", "chatbubble"],
  ["ellipsis", "ellipsis-horizontal"],
  ["pin", "pin"],
  ["list.bullet", "list"],
  ["square.stack", "layers-outline"],
  ["chevron.right", "chevron-forward"],
  ["checkmark", "checkmark"],
] as const;

describe("resolvePlatformSymbol", () => {
  it.each(symbolCases)(
    "maps %s to the Android-safe Ionicon %s",
    (symbolName, iconName) => {
      expect(resolvePlatformSymbol(symbolName)).toBe(iconName);
    },
  );

  it("keeps every platform symbol mapping non-empty", () => {
    for (const [symbolName] of symbolCases) {
      expect(resolvePlatformSymbol(symbolName)).toBeTruthy();
    }
  });
});
