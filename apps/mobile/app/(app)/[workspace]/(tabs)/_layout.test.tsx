import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  tabsScreens: [] as Array<Record<string, any>>,
  tabsProps: [] as Array<Record<string, any>>,
  anchorProps: [] as Array<Record<string, any>>,
  inboxUnread: 0,
  chatUnread: 0,
}));

vi.mock("react-native", () => ({
  View: (props: Record<string, any>) => React.createElement("View", props, props.children),
}));

vi.mock("expo-router", () => ({
  Tabs: Object.assign(
    (props: Record<string, any>) => {
      mocks.tabsProps.push(props);
      return React.createElement("Tabs", props, props.children);
    },
    {
      Screen: (props: Record<string, any>) => {
        mocks.tabsScreens.push(props);
        return React.createElement("TabsScreen", props);
      },
    },
  ),
}));

vi.mock("@/components/ui/platform-symbol", () => ({
  PlatformSymbol: (props: Record<string, any>) =>
    React.createElement("PlatformSymbol", props),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceId: string }) => unknown) =>
    selector({ currentWorkspaceId: "ws-1" }),
}));

vi.mock("@/lib/use-color-scheme", () => ({
  useColorScheme: () => ({ colorScheme: "light" }),
}));

vi.mock("@/lib/theme", () => ({
  THEME: {
    light: {
      brand: "#0066ff",
      foreground: "#111111",
      mutedForeground: "#666666",
      background: "#ffffff",
    },
  },
}));

vi.mock("@/lib/unread-counts", () => ({
  useInboxUnreadCount: () => mocks.inboxUnread,
  useChatUnreadSessionCount: () => mocks.chatUnread,
}));

vi.mock("@/components/nav/more-tab-dropdown", () => ({
  MoreTabDropdownAnchor: (props: Record<string, any>) => {
    mocks.anchorProps.push(props);
    if (props.triggerRef && "current" in props.triggerRef) {
      props.triggerRef.current = { open: vi.fn() };
    }
    return React.createElement("MoreTabDropdownAnchor", props);
  },
}));

import TabsLayout from "./_layout";

describe("TabsLayout", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.tabsScreens.length = 0;
    mocks.tabsProps.length = 0;
    mocks.anchorProps.length = 0;
    mocks.inboxUnread = 0;
    mocks.chatUnread = 0;
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("renders tab screens with capped badges", () => {
    mocks.inboxUnread = 120;
    mocks.chatUnread = 12;

    act(() => {
      renderer = create(<TabsLayout />);
    });

    const inbox = mocks.tabsScreens.find((screen) => screen.name === "inbox");
    const chat = mocks.tabsScreens.find((screen) => screen.name === "chat");
    const myIssues = mocks.tabsScreens.find((screen) => screen.name === "my-issues");
    const more = mocks.tabsScreens.find((screen) => screen.name === "more");
    expect(inbox?.options.tabBarBadge).toBe("99+");
    expect(chat?.options.tabBarBadge).toBe("9+");
    expect(more?.options.title).toBe("More");
    expect(mocks.anchorProps).toHaveLength(1);

    const inboxFilled = inbox?.options.tabBarIcon({
      color: "#111111",
      size: 20,
      focused: true,
    });
    const inboxOutline = inbox?.options.tabBarIcon({
      color: "#111111",
      size: 20,
      focused: false,
    });
    const issuesFilled = myIssues?.options.tabBarIcon({
      color: "#111111",
      size: 20,
      focused: true,
    });
    const issuesOutline = myIssues?.options.tabBarIcon({
      color: "#111111",
      size: 20,
      focused: false,
    });
    const chatFilled = chat?.options.tabBarIcon({
      color: "#111111",
      size: 20,
      focused: true,
    });
    const chatOutline = chat?.options.tabBarIcon({
      color: "#111111",
      size: 20,
      focused: false,
    });
    const moreIcon = more?.options.tabBarIcon({
      color: "#111111",
      size: 20,
    });

    expect(inboxFilled?.props.name).toBe("tray.fill");
    expect(inboxOutline?.props.name).toBe("tray");
    expect(issuesFilled?.props.name).toBe("checklist");
    expect(issuesOutline?.props.name).toBe("checklist.unchecked");
    expect(chatFilled?.props.name).toBe("bubble.left.fill");
    expect(chatOutline?.props.name).toBe("bubble.left");
    expect(moreIcon?.props.name).toBe("ellipsis");
  });

  it("hides zero-count badges and opens the more dropdown from the tab listener", () => {
    act(() => {
      renderer = create(<TabsLayout />);
    });

    const inbox = mocks.tabsScreens.find((screen) => screen.name === "inbox");
    const chat = mocks.tabsScreens.find((screen) => screen.name === "chat");
    const more = mocks.tabsScreens.find((screen) => screen.name === "more");
    expect(inbox?.options.tabBarBadge).toBeUndefined();
    expect(chat?.options.tabBarBadge).toBeUndefined();

    const preventDefault = vi.fn();
    act(() => {
      more?.listeners().tabPress({ preventDefault });
    });

    expect(preventDefault).toHaveBeenCalledTimes(1);
    expect(mocks.anchorProps[0]?.triggerRef.current.open).toHaveBeenCalledTimes(1);
  });
});
