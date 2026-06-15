import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  alertMock: vi.fn(),
  openUrl: vi.fn(),
  setStringAsync: vi.fn(),
  routerBack: vi.fn(),
  routerPush: vi.fn(),
  stackScreens: [] as Array<Record<string, unknown>>,
  iconButtons: [] as Array<Record<string, unknown>>,
  timelineProps: [] as Array<Record<string, unknown>>,
  composerProps: [] as Array<Record<string, unknown>>,
  detailRefetch: vi.fn(() => Promise.resolve()),
  invalidateQueries: vi.fn(() => Promise.resolve()),
  deleteMutate: vi.fn(),
  createPinMutate: vi.fn(),
  deletePinMutate: vi.fn(),
  showPlatformActionSheet: vi.fn(),
  pushViewedIssue: vi.fn(),
  clearCommentSelect: vi.fn(),
  clearReplyTarget: vi.fn(),
  issueRealtime: vi.fn(),
  queryState: {
    detail: {
      data: undefined as unknown,
      isLoading: false,
      error: null as unknown,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    },
    timeline: {
      data: undefined as unknown,
      isLoading: false,
      error: null as unknown,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    },
    pins: {
      data: undefined as unknown,
      isLoading: false,
      error: null as unknown,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    },
  },
}));

vi.mock("react-native", () => ({
  ActivityIndicator: (props: Record<string, unknown>) =>
    React.createElement("ActivityIndicator", props),
  Alert: { alert: mocks.alertMock },
  Linking: { openURL: mocks.openUrl },
  View: (props: Record<string, unknown>) =>
    React.createElement("View", props, props.children),
}));

vi.mock("expo-router", () => ({
  Stack: {
    Screen: (props: Record<string, unknown>) => {
      mocks.stackScreens.push(props);
      return React.createElement("StackScreen", props);
    },
  },
  router: {
    back: mocks.routerBack,
    push: mocks.routerPush,
  },
  useLocalSearchParams: () => ({
    id: "issue-1",
    workspace: "alpha",
    highlight: "comment-1",
    h: "nonce-1",
  }),
}));

vi.mock("expo-clipboard", () => ({
  setStringAsync: mocks.setStringAsync,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (arg: { kind: string }) => {
    if (arg.kind === "detail") return mocks.queryState.detail;
    if (arg.kind === "timeline") return mocks.queryState.timeline;
    return mocks.queryState.pins;
  },
  useQueryClient: () => ({
    invalidateQueries: mocks.invalidateQueries,
  }),
}));

vi.mock("@/components/ui/text", () => ({
  Text: (props: Record<string, unknown>) => React.createElement("Text", props),
}));

vi.mock("@/components/ui/button", () => ({
  Button: (props: Record<string, unknown>) => React.createElement("Button", props),
}));

vi.mock("@/components/ui/icon-button", () => ({
  IconButton: (props: Record<string, unknown>) => {
    mocks.iconButtons.push(props);
    return React.createElement("IconButton", props);
  },
}));

vi.mock("@/components/issue/timeline-list", () => ({
  TimelineList: (props: Record<string, unknown>) => {
    mocks.timelineProps.push(props);
    return React.createElement("TimelineList", props);
  },
}));

vi.mock("@/components/issue/agent-header-badge", () => ({
  AgentHeaderBadge: (props: Record<string, unknown>) =>
    React.createElement("AgentHeaderBadge", props),
}));

vi.mock("@/components/issue/inline-comment-composer", () => ({
  InlineCommentComposer: (props: Record<string, unknown>) => {
    mocks.composerProps.push(props);
    return React.createElement("InlineCommentComposer", props);
  },
}));

vi.mock("@/data/queries/issues", () => ({
  issueDetailOptions: (_wsId: string, _id: string) => ({ kind: "detail" }),
  issueTimelineOptions: (_wsId: string, _id: string) => ({ kind: "timeline" }),
  issueKeys: {
    timeline: (wsId: string, id: string) => ["issue", wsId, id, "timeline"],
  },
}));

vi.mock("@/data/mutations/issues", () => ({
  useDeleteIssue: () => ({
    mutate: mocks.deleteMutate,
  }),
}));

vi.mock("@/data/queries/pins", () => ({
  pinListOptions: () => ({ kind: "pins" }),
}));

vi.mock("@/data/mutations/pins", () => ({
  useCreatePin: () => ({ mutate: mocks.createPinMutate }),
  useDeletePin: () => ({ mutate: mocks.deletePinMutate }),
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: (selector: (state: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));

vi.mock("@/data/realtime/use-issue-realtime", () => ({
  useIssueRealtime: (...args: unknown[]) => mocks.issueRealtime(...args),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceId: string; currentWorkspaceSlug: string }) => unknown) =>
    selector({ currentWorkspaceId: "ws-1", currentWorkspaceSlug: "alpha" }),
}));

vi.mock("@/data/viewed-issues-store", () => ({
  useViewedIssuesStore: {
    getState: () => ({
      push: mocks.pushViewedIssue,
    }),
  },
}));

vi.mock("@/data/comment-select-store", () => ({
  useCommentSelectStore: {
    getState: () => ({
      clear: mocks.clearCommentSelect,
    }),
  },
}));

vi.mock("@/data/stores/reply-target-store", () => ({
  useReplyTargetStore: {
    getState: () => ({
      clear: mocks.clearReplyTarget,
    }),
  },
}));

vi.mock("@/lib/action-sheet", () => ({
  showPlatformActionSheet: mocks.showPlatformActionSheet,
}));

import IssueDetail from "./[id]";

describe("IssueDetail", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.alertMock.mockReset();
    mocks.openUrl.mockReset();
    mocks.setStringAsync.mockReset();
    mocks.routerBack.mockReset();
    mocks.routerPush.mockReset();
    mocks.stackScreens.length = 0;
    mocks.iconButtons.length = 0;
    mocks.timelineProps.length = 0;
    mocks.composerProps.length = 0;
    mocks.invalidateQueries.mockReset();
    mocks.deleteMutate.mockReset();
    mocks.createPinMutate.mockReset();
    mocks.deletePinMutate.mockReset();
    mocks.showPlatformActionSheet.mockReset();
    mocks.pushViewedIssue.mockReset();
    mocks.clearCommentSelect.mockReset();
    mocks.clearReplyTarget.mockReset();
    mocks.issueRealtime.mockReset();
    mocks.queryState.detail = {
      data: undefined,
      isLoading: false,
      error: null,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    };
    mocks.queryState.timeline = {
      data: [],
      isLoading: false,
      error: null,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    };
    mocks.queryState.pins = {
      data: [],
      isLoading: false,
      error: null,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    };
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("renders loading and error states", () => {
    mocks.queryState.detail.isLoading = true;

    act(() => {
      renderer = create(<IssueDetail />);
    });

    expect(renderer!.root.findAllByType("ActivityIndicator")).toHaveLength(1);

    act(() => {
      renderer?.unmount();
      mocks.queryState.detail.isLoading = false;
      mocks.queryState.detail.error = new Error("boom");
      renderer = create(<IssueDetail />);
    });

    const textValues = renderer!.root
      .findAllByType("Text")
      .flatMap((node) =>
        Array.isArray(node.props.children)
          ? node.props.children
          : [node.props.children],
      )
      .filter((value): value is string => typeof value === "string");

    expect(textValues.some((value) => value.includes("Failed to load issue"))).toBe(true);
    act(() => {
      renderer!.root.findByType("Button").props.onPress();
    });
    expect(mocks.queryState.detail.refetch).toHaveBeenCalledTimes(1);
  });

  it("renders the success state, tracks viewing, and cleans up on unmount", async () => {
    mocks.queryState.detail.data = {
      id: "issue-1",
      identifier: "MUL-1",
    };
    mocks.queryState.timeline.data = [{ id: "entry-1" }];

    await act(async () => {
      renderer = create(<IssueDetail />);
    });

    expect(mocks.pushViewedIssue).toHaveBeenCalledWith("ws-1", "issue-1");
    expect(mocks.issueRealtime).toHaveBeenCalledWith(
      "issue-1",
      expect.any(Function),
    );
    expect(mocks.timelineProps).toHaveLength(1);
    expect(mocks.composerProps).toEqual([{ issueId: "issue-1" }]);

    await act(async () => {
      await (mocks.timelineProps[0] as { onRefresh: () => Promise<void> }).onRefresh();
    });

    expect(mocks.queryState.detail.refetch).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["issue", "ws-1", "issue-1", "timeline"],
    });

    act(() => {
      renderer?.unmount();
    });

    expect(mocks.clearCommentSelect).toHaveBeenCalled();
    expect(mocks.clearReplyTarget).toHaveBeenCalled();
  });

  it("shows issue actions and executes pin/link/delete handlers", () => {
    mocks.queryState.detail.data = {
      id: "issue-1",
      identifier: "MUL-1",
    };
    mocks.queryState.timeline.data = [{ id: "entry-1" }];
    process.env.EXPO_PUBLIC_WEB_URL = "https://app.multica.ai";

    act(() => {
      renderer = create(<IssueDetail />);
    });

    const headerRight = mocks.stackScreens[0]?.options.headerRight as
      | (() => React.ReactElement)
      | undefined;
    expect(headerRight).toBeTypeOf("function");

    act(() => {
      create(headerRight!());
    });

    act(() => {
      (mocks.iconButtons[0] as { onPress: () => void }).onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Pin",
      "Edit details",
      "Copy link",
      "Open on web",
      "Delete issue",
      "Cancel",
    ]);

    act(() => {
      config.options[0].onPress();
      config.options[1].onPress();
      config.options[2].onPress();
      config.options[3].onPress();
      config.options[4].onPress();
    });

    expect(mocks.createPinMutate).toHaveBeenCalledWith({
      item_type: "issue",
      item_id: "issue-1",
    });
    expect(mocks.routerPush).toHaveBeenCalledWith("/alpha/issue/issue-1/edit");
    expect(mocks.setStringAsync).toHaveBeenCalledWith(
      "https://app.multica.ai/alpha/issue/MUL-1",
    );
    expect(mocks.openUrl).toHaveBeenCalledWith(
      "https://app.multica.ai/alpha/issue/MUL-1",
    );
    expect(mocks.alertMock).toHaveBeenCalledTimes(1);

    const deleteButtons = mocks.alertMock.mock.calls[0]?.[2];
    act(() => {
      deleteButtons[1].onPress();
    });

    const deleteCall = mocks.deleteMutate.mock.calls[0];
    expect(deleteCall?.[0]).toBe("issue-1");
    expect(deleteCall?.[1]).toEqual(
      expect.objectContaining({
        onSuccess: expect.any(Function),
      }),
    );
    act(() => {
      deleteCall?.[1]?.onSuccess();
    });
    expect(mocks.routerBack).toHaveBeenCalledTimes(1);
  });

  it("handles pinned issues and missing web links", () => {
    let menuRenderer: ReactTestRenderer | null = null;
    mocks.queryState.detail.data = {
      id: "issue-2",
      identifier: "MUL-2",
    };
    mocks.queryState.pins.data = [
      { item_type: "issue", item_id: "issue-2" },
    ];
    delete process.env.EXPO_PUBLIC_WEB_URL;

    act(() => {
      renderer = create(<IssueDetail />);
    });

    const headerRight = mocks.stackScreens[0]?.options.headerRight as
      | (() => React.ReactElement)
      | undefined;
    act(() => {
      mocks.iconButtons.length = 0;
      menuRenderer = create(headerRight!());
    });
    act(() => {
      (mocks.iconButtons.at(-1) as { onPress: () => void }).onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Unpin",
      "Edit details",
      "Delete issue",
      "Cancel",
    ]);
    act(() => {
      config.options[0].onPress();
    });
    expect(mocks.deletePinMutate).toHaveBeenCalledWith({
      itemType: "issue",
      itemId: "issue-2",
    });
    menuRenderer?.unmount();
  });

  it("renders the not-found fallback when detail data is missing", () => {
    mocks.queryState.detail.data = undefined;
    mocks.queryState.detail.error = null;

    act(() => {
      renderer = create(<IssueDetail />);
    });

    const textValues = renderer!.root
      .findAllByType("Text")
      .flatMap((node) =>
        Array.isArray(node.props.children)
          ? node.props.children
          : [node.props.children],
      )
      .filter((value): value is string => typeof value === "string");
    expect(textValues).toContain("not found");
  });
});
