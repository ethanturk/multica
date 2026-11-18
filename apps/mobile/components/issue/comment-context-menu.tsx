/**
 * Native long-press actions for a comment bubble, with a pressed highlight
 * while the sheet is visible. Reply targets the inline composer.
 *
 * Present nested reaction sheets from the outer sheet's completion callback;
 * iOS cannot present the next ActionSheet while the first is dismissing.
 */
import { useCallback, useState } from "react";
import { Alert } from "react-native";
import { router } from "expo-router";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import { useQuery } from "@tanstack/react-query";
import type { Reaction, TimelineEntry } from "@multica/core/types";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  commentDeleteKeepsReplies,
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
} from "@/data/mutations/issues";
import { appConfigOptions } from "@/data/queries/billing";
import { showPlatformActionSheet } from "@/lib/action-sheet";
import { QUICK_EMOJIS } from "@/lib/quick-emojis";
import { i18n, useT } from "@/lib/i18n";

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
  // Same config cache useDeleteComment reads when it runs, so the copy and
  // the delete route agree.
  const { data: keepReplies = false } = useQuery({
    ...appConfigOptions(),
    select: commentDeleteKeepsReplies,
  });
  const { t } = useT("issues");

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
          label: t("comments.reply"),
          onPress: () => {
            // Set the reply target — the InlineCommentComposer subscribes
            // to this store, auto-expands, and threads the next submit
            // under entry.id via useCreateComment's `parentId`.
            const actorName =
              entry.actor_name ||
              getName(
                entry.actor_type as "member" | "agent" | null | undefined,
                entry.actor_id,
              );
            useReplyTargetStore.getState().setTarget({
              commentId: entry.id,
              actorName: actorName || t("comments.fallback_actor"),
              preview: entry.content ?? "",
            });
          },
        },
        {
          label: t("comments.react"),
          // Present the nested React sheet from inside this completion
          // callback — see file header for why.
          onPress: () =>
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
            }),
        },
        ...(hasContent
          ? [
              {
                label: t("comments.copy"),
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
                label: t("comments.select_text"),
                onPress: () => {
                  useCommentSelectStore.getState().setSelecting(entry.id);
                },
              },
            ]
          : []),
        ...(canCopyLink
          ? [
              {
                label: t("menu.copy_link"),
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
                label: resolved ? t("comments.unresolve") : t("comments.resolve"),
                onPress: () =>
                  resolveComment.mutate({
                    commentId: entry.id,
                    resolved: !entry.resolved_at,
                  }),
              },
            ]
          : []),
        ...(isOwn
          ? [
              {
                label: t("common:actions.delete"),
                style: "destructive" as const,
                onPress: () =>
                  Alert.alert(
                    t("comments.delete_title"),
                    // Promise kept replies only when the server declares it
                    // (#8296); older servers delete the replies too.
                    keepReplies
                      ? t("comments.delete_keep_replies")
                      : t("comments.delete_with_replies"),
                    [
                      { text: t("common:actions.cancel"), style: "cancel" },
                      {
                        text: t("common:actions.delete"),
                        style: "destructive",
                        onPress: () => deleteComment.mutate(entry.id),
                      },
                    ],
                  ),
              },
            ]
          : []),
        { label: t("common:actions.cancel"), style: "cancel" as const },
      ],
      onDismiss: () => setIsPressed(false),
    });
  }, [
    entry,
    issueId,
    issueIdentifier,
    userId,
    wsSlug,
    toggleReaction,
    deleteComment,
    resolveComment,
    getName,
    keepReplies,
    t,
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
  const t = i18n.t.bind(i18n);
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
        label: t("issues:comments.more_reactions"),
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
      { label: t("common:actions.cancel"), style: "cancel" as const },
    ],
  });
}
