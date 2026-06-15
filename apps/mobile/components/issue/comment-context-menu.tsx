/**
 * Long-press handler for a comment bubble. Exposes `onLongPress` (drives a
 * native iOS ActionSheetIOS) and `isPressed` (drives the caller's highlight
 * ring while the sheet is on screen).
 *
 * iOS-native first per apps/mobile/CLAUDE.md §UI components → waterfall step
 * 1: `ActionSheetIOS.showActionSheetWithOptions`. Zero custom layout, zero
 * animation, zero overflow math, zero new deps.
 *
 * Item set (conditional, mirrors web's comment context menu):
 *   Reply (stub) · React… (opens nested sheet) · Copy · Select Text ·
 *   Copy Link · Resolve/Unresolve Thread (root only) · Delete (own only) ·
 *   Cancel
 *
 * The nested React… sheet (5 quick emojis + More reactions… + Cancel) is
 * fired from INSIDE the outer sheet's completion callback rather than
 * inline, because iOS will refuse to present a second ActionSheet while the
 * first is still dismissing — the callback runs after dismissal completes.
 */
import { useCallback, useState } from "react";
import { Alert } from "react-native";
import { router } from "expo-router";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
} from "@/data/mutations/issues";
import { showPlatformActionSheet } from "@/lib/action-sheet";
import { QUICK_EMOJIS } from "@/lib/quick-emojis";

const QUICK_ROW_SIZE = 5;

export function useCommentLongPress(
  entry: TimelineEntry,
  issueId: string,
  issueIdentifier: string | undefined,
): { onLongPress: () => void; isPressed: boolean } {
  const [isPressed, setIsPressed] = useState(false);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id);
  const toggleReaction = useToggleCommentReaction(issueId);
  const deleteComment = useDeleteComment(issueId);
  const resolveComment = useResolveComment(issueId);
  const { getName } = useActorLookup();

  const onLongPress = useCallback(() => {
    const isOwn = entry.actor_type === "member" && entry.actor_id === userId;
    const isRoot = !entry.parent_id;
    const resolved = !!entry.resolved_at;
    const hasContent = !!entry.content;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const canCopyLink = !!(webUrl && wsSlug && issueIdentifier);
    const reactions = (entry.reactions ?? []) as Reaction[];

    Haptics.selectionAsync().catch(() => {});
    setIsPressed(true);

    showPlatformActionSheet({
      options: [
        {
          label: "Reply",
          onPress: () => {
            const actorName = getName(
              entry.actor_type as "member" | "agent" | null | undefined,
              entry.actor_id,
            );
            useReplyTargetStore.getState().setTarget({
              commentId: entry.id,
              actorName: actorName || "comment",
              preview: entry.content ?? "",
            });
          },
        },
        {
          label: "React…",
          onPress: () => {
            presentReactSheet({
              entry,
              reactions,
              userId,
              wsSlug,
              issueId,
              toggle: (emoji, existing) =>
                toggleReaction.mutate({
                  commentId: entry.id,
                  emoji,
                  existing,
                }),
            });
          },
        },
        ...(hasContent
          ? [
              {
                label: "Copy",
                onPress: () => {
                  if (entry.content) {
                    Clipboard.setStringAsync(entry.content);
                    Haptics.notificationAsync(
                      Haptics.NotificationFeedbackType.Success,
                    ).catch(() => {});
                  }
                },
              },
              {
                label: "Select Text",
                onPress: () => {
                  useCommentSelectStore.getState().setSelecting(entry.id);
                },
              },
            ]
          : []),
        ...(canCopyLink
          ? [
              {
                label: "Copy Link",
                onPress: () => {
                  const url = `${webUrl}/${wsSlug}/issue/${issueIdentifier}#comment-${entry.id}`;
                  Clipboard.setStringAsync(url);
                  Haptics.notificationAsync(
                    Haptics.NotificationFeedbackType.Success,
                  ).catch(() => {});
                },
              },
            ]
          : []),
        ...(isRoot
          ? [
              {
                label: resolved ? "Unresolve Thread" : "Resolve Thread",
                onPress: () => {
                  resolveComment.mutate({
                    commentId: entry.id,
                    resolved: !entry.resolved_at,
                  });
                },
              },
            ]
          : []),
        ...(isOwn
          ? [
              {
                label: "Delete",
                style: "destructive" as const,
                onPress: () => {
                  Alert.alert(
                    "Delete comment?",
                    "This comment will be permanently deleted. Replies in the thread will also be removed. This cannot be undone.",
                    [
                      { text: "Cancel", style: "cancel" },
                      {
                        text: "Delete",
                        style: "destructive",
                        onPress: () => deleteComment.mutate(entry.id),
                      },
                    ],
                  );
                },
              },
            ]
          : []),
        { label: "Cancel", style: "cancel" },
      ],
      onDismiss: () => setIsPressed(false),
    });
  }, [
    entry,
    issueId,
    issueIdentifier,
    userId,
    wsSlug,
    getName,
    toggleReaction,
    deleteComment,
    resolveComment,
  ]);

  return { onLongPress, isPressed };
}

function presentReactSheet(args: {
  entry: TimelineEntry;
  reactions: Reaction[];
  userId: string | undefined;
  wsSlug: string | null;
  issueId: string;
  toggle: (emoji: string, existing: Reaction | undefined) => void;
}) {
  const { entry, reactions, userId, wsSlug, issueId, toggle } = args;
  const emojis = QUICK_EMOJIS.slice(0, QUICK_ROW_SIZE);
  showPlatformActionSheet({
    options: [
      ...emojis.map((emoji) => ({
        label: emoji,
        onPress: () => {
          const existing = reactions.find(
            (reaction) =>
              reaction.emoji === emoji &&
              reaction.actor_type === "member" &&
              reaction.actor_id === userId,
          );
          toggle(emoji, existing);
        },
      })),
      {
        label: "More reactions…",
        onPress: () => {
          if (!wsSlug) return;
          router.push({
            pathname:
              "/[workspace]/issue/[id]/comment/[commentId]/emoji-picker",
            params: {
              workspace: wsSlug,
              id: issueId,
              commentId: entry.id,
            },
          });
        },
      },
      { label: "Cancel", style: "cancel" },
    ],
  });
}
