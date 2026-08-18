import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

import { useCommentLongPress } from "./comment-context-menu";

const mocks = vi.hoisted(() => ({
  alertMock: vi.fn(),
  routerPush: vi.fn(),
  setStringAsync: vi.fn(),
  selectionAsync: vi.fn(() => Promise.resolve()),
  notificationAsync: vi.fn(() => Promise.resolve()),
  setSelecting: vi.fn(),
  setReplyTarget: vi.fn(),
  getName: vi.fn(() => "Reviewer"),
  deleteMutate: vi.fn(),
  resolveMutate: vi.fn(),
  toggleMutate: vi.fn(),
  showPlatformActionSheet: vi.fn(),
  currentWorkspaceSlug: "alpha" as string | null,
}));

vi.mock("react-native", () => ({
  Alert: { alert: mocks.alertMock },
}));

vi.mock("expo-router", () => ({
  router: { push: mocks.routerPush },
}));

vi.mock("expo-clipboard", () => ({
  setStringAsync: mocks.setStringAsync,
}));

vi.mock("expo-haptics", () => ({
  selectionAsync: mocks.selectionAsync,
  notificationAsync: mocks.notificationAsync,
  NotificationFeedbackType: {
    Success: "success",
  },
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceSlug: string | null }) => unknown) =>
    selector({ currentWorkspaceSlug: mocks.currentWorkspaceSlug }),
}));

vi.mock("@/data/comment-select-store", () => ({
  useCommentSelectStore: {
    getState: () => ({
      setSelecting: mocks.setSelecting,
    }),
  },
}));

vi.mock("@/data/stores/reply-target-store", () => ({
  useReplyTargetStore: {
    getState: () => ({
      setTarget: mocks.setReplyTarget,
    }),
  },
}));

vi.mock("@/data/use-actor-name", () => ({
  useActorLookup: () => ({
    getName: mocks.getName,
  }),
}));

vi.mock("@/data/mutations/issues", () => ({
  useDeleteComment: () => ({ mutate: mocks.deleteMutate }),
  useResolveComment: () => ({ mutate: mocks.resolveMutate }),
  useToggleCommentReaction: () => ({ mutate: mocks.toggleMutate }),
}));

vi.mock("@/lib/action-sheet", () => ({
  showPlatformActionSheet: mocks.showPlatformActionSheet,
}));

describe("useCommentLongPress", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
     
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.alertMock.mockReset();
    mocks.routerPush.mockReset();
    mocks.setStringAsync.mockReset();
    mocks.selectionAsync.mockClear();
    mocks.notificationAsync.mockClear();
    mocks.setSelecting.mockReset();
    mocks.setReplyTarget.mockReset();
    mocks.getName.mockReset();
    mocks.getName.mockReturnValue("Reviewer");
    mocks.deleteMutate.mockReset();
    mocks.resolveMutate.mockReset();
    mocks.toggleMutate.mockReset();
    mocks.showPlatformActionSheet.mockReset();
    mocks.currentWorkspaceSlug = "alpha";
    process.env.EXPO_PUBLIC_WEB_URL = "https://app.multica.ai";
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("builds the full action set and executes primary handlers", () => {
    let captured:
      | ReturnType<typeof useCommentLongPress>
      | undefined;

    function Harness() {
      captured = useCommentLongPress(
        {
          id: "comment-1",
          actor_type: "member",
          actor_id: "member-1",
          parent_id: null,
          resolved_at: null,
          content: "Hello world",
          reactions: [
            {
              emoji: "👍",
              actor_type: "member",
              actor_id: "member-1",
            },
          ],
        } as never,
        "issue-1",
        "MUL-1",
      );
      return null;
    }

    act(() => {
      renderer = create(<Harness />);
    });

    act(() => {
      captured?.onLongPress();
    });

    expect(mocks.selectionAsync).toHaveBeenCalledTimes(1);
    expect(mocks.showPlatformActionSheet).toHaveBeenCalledTimes(1);
    const outerConfig = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(
      outerConfig.options.map((option: { label: string }) => option.label),
    ).toEqual([
      "Reply",
      "React…",
      "Copy",
      "Select Text",
      "Copy Link",
      "Resolve Thread",
      "Delete",
      "Cancel",
    ]);

    act(() => {
      outerConfig.options[0].onPress();
      outerConfig.options[2].onPress();
      outerConfig.options[3].onPress();
      outerConfig.options[4].onPress();
      outerConfig.options[5].onPress();
      outerConfig.options[6].onPress();
      outerConfig.onDismiss();
    });

    expect(mocks.setReplyTarget).toHaveBeenCalledWith({
      commentId: "comment-1",
      actorName: "Reviewer",
      preview: "Hello world",
    });
    expect(mocks.setStringAsync).toHaveBeenCalledWith("Hello world");
    expect(mocks.setSelecting).toHaveBeenCalledWith("comment-1");
    expect(mocks.setStringAsync).toHaveBeenCalledWith(
      "https://app.multica.ai/alpha/issue/MUL-1#comment-comment-1",
    );
    expect(mocks.resolveMutate).toHaveBeenCalledWith({
      commentId: "comment-1",
      resolved: true,
    });
    expect(mocks.alertMock).toHaveBeenCalledTimes(1);

    const deleteButtons = mocks.alertMock.mock.calls[0]?.[2];
    act(() => {
      deleteButtons[1].onPress();
    });
    expect(mocks.deleteMutate).toHaveBeenCalledWith("comment-1");
  });

  it("handles quick reactions, more reactions navigation, and reduced options", () => {
    let captured:
      | ReturnType<typeof useCommentLongPress>
      | undefined;

    function Harness() {
      captured = useCommentLongPress(
        {
          id: "comment-2",
          actor_type: "agent",
          actor_id: "agent-1",
          parent_id: "comment-1",
          resolved_at: "2026-06-15T00:00:00Z",
          content: "",
          reactions: [],
        } as never,
        "issue-2",
        undefined,
      );
      return null;
    }

    act(() => {
      renderer = create(<Harness />);
    });

    act(() => {
      captured?.onLongPress();
    });

    const outerConfig = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(
      outerConfig.options.map((option: { label: string }) => option.label),
    ).toEqual(["Reply", "React…", "Cancel"]);

    act(() => {
      outerConfig.options[1].onPress();
    });

    const reactionConfig = mocks.showPlatformActionSheet.mock.calls[1]?.[0];
    expect(
      reactionConfig.options.map((option: { label: string }) => option.label),
    ).toEqual(["👍", "👌", "❤️", "✅", "🎉", "More reactions…", "Cancel"]);

    act(() => {
      reactionConfig.options[0].onPress();
      reactionConfig.options[5].onPress();
    });

    expect(mocks.toggleMutate).toHaveBeenCalledWith({
      commentId: "comment-2",
      emoji: "👍",
      existing: undefined,
    });
    expect(mocks.routerPush).toHaveBeenCalledWith({
      pathname: "/[workspace]/issue/[id]/comment/[commentId]/emoji-picker",
      params: {
        workspace: "alpha",
        id: "issue-2",
        commentId: "comment-2",
      },
    });
  });

  it("passes through an existing matching reaction and skips emoji navigation without a workspace slug", () => {
    let captured:
      | ReturnType<typeof useCommentLongPress>
      | undefined;
    mocks.currentWorkspaceSlug = null;

    function Harness() {
      captured = useCommentLongPress(
        {
          id: "comment-3",
          actor_type: "agent",
          actor_id: "agent-1",
          parent_id: "comment-1",
          resolved_at: null,
          content: "",
          reactions: [
            {
              emoji: "👍",
              actor_type: "member",
              actor_id: "member-1",
            },
          ],
        } as never,
        "issue-3",
        undefined,
      );
      return null;
    }

    act(() => {
      renderer = create(<Harness />);
    });

    act(() => {
      captured?.onLongPress();
    });

    const outerConfig = mocks.showPlatformActionSheet.mock.calls.at(-1)?.[0];
    act(() => {
      outerConfig.options[1].onPress();
    });
    const reactionConfig = mocks.showPlatformActionSheet.mock.calls.at(-1)?.[0];
    act(() => {
      reactionConfig.options[0].onPress();
      reactionConfig.options[5].onPress();
    });
    expect(mocks.toggleMutate).toHaveBeenCalledWith({
      commentId: "comment-3",
      emoji: "👍",
      existing: {
        emoji: "👍",
        actor_type: "member",
        actor_id: "member-1",
      },
    });
  });
});
