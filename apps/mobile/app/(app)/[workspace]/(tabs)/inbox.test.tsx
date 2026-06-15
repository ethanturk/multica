import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

import Inbox from "./inbox";

function MockView(props: Record<string, any>) {
  return React.createElement("View", props);
}

const mocks = vi.hoisted(() => ({
  alertMock: vi.fn(),
  queryState: {
    data: undefined as unknown,
    isLoading: false,
    error: null as unknown,
    refetch: vi.fn(),
    isRefetching: false,
  },
  routerPush: vi.fn(),
  markReadMutate: vi.fn(),
  markAllReadMutate: vi.fn(),
  archiveMutate: vi.fn(),
  archiveAllMutate: vi.fn(),
  archiveAllReadMutate: vi.fn(),
  archiveCompletedMutate: vi.fn(),
  showPlatformActionSheet: vi.fn(),
  iconButtons: [] as Record<string, any>[],
  rowProps: [] as Record<string, any>[],
}));

vi.mock("react-native", () => ({
  Alert: { alert: mocks.alertMock },
  FlatList: (props: Record<string, any>) => {
    mocks.rowProps.push({ flatListProps: props });
    return React.createElement("FlatList", props);
  },
  View: MockView,
}));

vi.mock("expo-router", () => ({
  router: { push: mocks.routerPush },
}));

vi.mock("@expo/vector-icons", () => ({
  Ionicons: (props: Record<string, any>) =>
    React.createElement("Ionicons", props),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => mocks.queryState,
}));

vi.mock("@/components/ui/text", () => ({
  Text: (props: Record<string, any>) => React.createElement("Text", props),
}));

vi.mock("@/components/ui/button", () => ({
  Button: (props: Record<string, any>) => React.createElement("Button", props),
}));

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: (props: Record<string, any>) => React.createElement("Skeleton", props),
}));

vi.mock("@/components/ui/header", () => ({
  Header: (props: Record<string, any>) =>
    React.createElement(
      "Header",
      props,
      props.right as React.ReactNode,
    ),
}));

vi.mock("@/components/ui/icon-button", () => ({
  IconButton: (props: Record<string, any>) => {
    mocks.iconButtons.push(props);
    return React.createElement("IconButton", props);
  },
}));

vi.mock("@/components/ui/app-header-actions", () => ({
  HeaderActions: () => React.createElement("HeaderActions"),
}));

vi.mock("@/components/inbox/swipeable-inbox-row", () => ({
  SwipeableInboxRow: (props: Record<string, any>) => {
    mocks.rowProps.push(props);
    return React.createElement("SwipeableInboxRow", props);
  },
}));

vi.mock("@/data/queries/inbox", () => ({
  inboxListOptions: (wsId: string | null) => ({ wsId }),
}));

vi.mock("@/data/mutations/inbox", () => ({
  useArchiveAllInbox: () => ({ mutate: mocks.archiveAllMutate }),
  useArchiveAllReadInbox: () => ({ mutate: mocks.archiveAllReadMutate }),
  useArchiveCompletedInbox: () => ({ mutate: mocks.archiveCompletedMutate }),
  useArchiveInbox: () => ({ mutate: mocks.archiveMutate }),
  useMarkAllInboxRead: () => ({ mutate: mocks.markAllReadMutate }),
  useMarkInboxRead: () => ({ mutate: mocks.markReadMutate }),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceId: string | null; currentWorkspaceSlug: string | null }) => unknown) =>
    selector({ currentWorkspaceId: "ws-1", currentWorkspaceSlug: "alpha" }),
}));

vi.mock("@/lib/use-color-scheme", () => ({
  useColorScheme: () => ({ colorScheme: "light" }),
}));

vi.mock("@/lib/theme", () => ({
  THEME: {
    light: { mutedForeground: "#999" },
  },
}));

vi.mock("@/lib/action-sheet", () => ({
  showPlatformActionSheet: mocks.showPlatformActionSheet,
}));

vi.mock("@/lib/inbox-display", () => ({
  deduplicateInboxItems: (items: unknown[]) => items,
}));

describe("Inbox", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // react-test-renderer on React 19 requires the act environment flag.
     
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.iconButtons.length = 0;
    mocks.rowProps.length = 0;
    mocks.alertMock.mockReset();
    mocks.routerPush.mockReset();
    mocks.markReadMutate.mockReset();
    mocks.markAllReadMutate.mockReset();
    mocks.archiveMutate.mockReset();
    mocks.archiveAllMutate.mockReset();
    mocks.archiveAllReadMutate.mockReset();
    mocks.archiveCompletedMutate.mockReset();
    mocks.showPlatformActionSheet.mockReset();
    mocks.queryState.data = undefined;
    mocks.queryState.isLoading = false;
    mocks.queryState.error = null;
    mocks.queryState.refetch = vi.fn();
    mocks.queryState.isRefetching = false;
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("renders the loading skeleton state", () => {
    mocks.queryState.isLoading = true;

    act(() => {
      renderer = create(<Inbox />);
    });

    const tree = renderer!.root;
    expect(tree.findAllByType("Skeleton")).toHaveLength(18);
  });

  it("renders error and retry states", () => {
    mocks.queryState.error = new Error("boom");

    act(() => {
      renderer = create(<Inbox />);
    });

    const textValues = renderer!.root
      .findAllByType("Text")
      .flatMap((node) =>
        Array.isArray(node.props.children)
          ? node.props.children
          : [node.props.children],
      )
      .filter((value): value is string => typeof value === "string");
    expect(textValues.some((value) => value.includes("Failed to load inbox"))).toBe(true);

    const button = renderer!.root.findByType("Button");
    act(() => {
      button.props.onPress();
    });
    expect(mocks.queryState.refetch).toHaveBeenCalledTimes(1);
  });

  it("renders the empty state", () => {
    mocks.queryState.data = [];

    act(() => {
      renderer = create(<Inbox />);
    });

    const textValues = renderer!.root
      .findAllByType("Text")
      .flatMap((node) => (Array.isArray(node.props.children) ? node.props.children : [node.props.children]));
    expect(textValues).toContain("Inbox zero");
  });

  it("renders rows and handles navigation and menu actions", () => {
    mocks.queryState.data = [
      {
        id: "item-1",
        read: false,
        issue_id: "issue-1",
        details: { comment_id: "comment-1" },
      },
    ];

    act(() => {
      renderer = create(<Inbox />);
    });

    expect(mocks.iconButtons).toHaveLength(1);

    act(() => {
      (mocks.iconButtons[0] as { onPress: () => void }).onPress();
    });

    const menuConfig = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(menuConfig.options.map((option: { label: string }) => option.label)).toEqual([
      "Mark all read",
      "Archive all read",
      "Archive completed",
      "Archive all",
      "Cancel",
    ]);

    act(() => {
      menuConfig.options[0].onPress();
      menuConfig.options[1].onPress();
      menuConfig.options[2].onPress();
      menuConfig.options[3].onPress();
    });

    expect(mocks.markAllReadMutate).toHaveBeenCalledTimes(1);
    expect(mocks.archiveAllReadMutate).toHaveBeenCalledTimes(1);
    expect(mocks.archiveCompletedMutate).toHaveBeenCalledTimes(1);
    expect(mocks.alertMock).toHaveBeenCalledTimes(1);

    const deleteButtons = mocks.alertMock.mock.calls[0]?.[2];
    act(() => {
      deleteButtons[1].onPress();
    });
    expect(mocks.archiveAllMutate).toHaveBeenCalledTimes(1);

    const flatList = mocks.rowProps.find((prop) => "flatListProps" in prop) as {
      flatListProps: {
        keyExtractor: (item: { id: string }) => string;
        ItemSeparatorComponent: () => React.ReactElement;
        renderItem: (args: { item: unknown }) => React.ReactElement<{
          item: { id: string };
          onPress: () => void;
          onArchive: () => void;
        }>;
      };
    };
    const renderedRow = flatList.flatListProps.renderItem({
      item: (mocks.queryState.data as { id: string }[])[0],
    }).props as {
      item: { id: string };
      onPress: () => void;
      onArchive: () => void;
    };

    act(() => {
      renderedRow.onPress();
      renderedRow.onArchive();
    });

    expect(flatList.flatListProps.keyExtractor({ id: "item-1" })).toBe("item-1");
    expect(flatList.flatListProps.ItemSeparatorComponent().type).toBe(MockView);
    expect(mocks.markReadMutate).toHaveBeenCalledWith("item-1");
    expect(mocks.routerPush).toHaveBeenCalledTimes(1);
    expect(mocks.archiveMutate).toHaveBeenCalledWith("item-1");
  });
});
