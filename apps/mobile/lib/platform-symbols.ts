import type { ComponentProps } from "react";
import { Ionicons } from "@expo/vector-icons";

export type PlatformSymbolName =
  | "tray"
  | "tray.fill"
  | "checklist"
  | "checklist.unchecked"
  | "bubble.left"
  | "bubble.left.fill"
  | "ellipsis"
  | "pin"
  | "list.bullet"
  | "square.stack"
  | "chevron.right"
  | "checkmark";

const SYMBOL_MAP = {
  tray: "file-tray-outline",
  "tray.fill": "file-tray",
  checklist: "checkmark-circle",
  "checklist.unchecked": "checkmark-circle-outline",
  "bubble.left": "chatbubble-outline",
  "bubble.left.fill": "chatbubble",
  ellipsis: "ellipsis-horizontal",
  pin: "pin",
  "list.bullet": "list",
  "square.stack": "layers-outline",
  "chevron.right": "chevron-forward",
  checkmark: "checkmark",
} satisfies Record<PlatformSymbolName, ComponentProps<typeof Ionicons>["name"]>;

export function resolvePlatformSymbol(
  name: PlatformSymbolName,
): ComponentProps<typeof Ionicons>["name"] {
  return SYMBOL_MAP[name];
}

