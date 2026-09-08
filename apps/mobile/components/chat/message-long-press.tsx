/**
 * Long-press handler for a chat message bubble. Exposes `onLongPress`
 * (drives a native iOS ActionSheetIOS) and `isPressed` (drives the
 * caller's highlight ring while the sheet is on screen).
 *
 * iOS-native first per apps/mobile/CLAUDE.md §UI components → waterfall
 * step 1: `ActionSheetIOS.showActionSheetWithOptions`. Zero custom
 * layout, zero animation, zero overflow math, zero new deps.
 *
 * Item set (v1, conditional):
 *   Copy · Select Text · Cancel
 *
 * Mirrors `useCommentLongPress` in `components/issue/comment-context-
 * menu.tsx` — kept as a sibling rather than a shared primitive because
 * we have only 2 callers (chat + comments). Below the "3 callers + no
 * native alternative" threshold in apps/mobile/CLAUDE.md.
 */
import { useCallback, useState } from "react";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { ChatMessage } from "@multica/core/types";
import { useChatSelectStore } from "@/data/chat-select-store";
import { showPlatformActionSheet } from "@/lib/action-sheet";

export function useChatMessageLongPress(
  message: ChatMessage,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);

  const onLongPress = useCallback(() => {
    const hasContent = !!message.content;

    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    showPlatformActionSheet({
      options: [
        ...(hasContent
          ? [
              {
                label: "Copy",
                onPress: () => {
                  if (message.content) {
                    Clipboard.setStringAsync(message.content);
                    Haptics.notificationAsync(
                      Haptics.NotificationFeedbackType.Success,
                    ).catch(() => {});
                  }
                },
              },
              {
                label: "Select Text",
                onPress: () => {
                  useChatSelectStore.getState().setSelecting(message.id);
                },
              },
            ]
          : []),
        { label: "Cancel", style: "cancel" },
      ],
      onDismiss: () => setIsPressed(false),
    });
  }, [message]);

  return { onLongPress, isPressed };
}
