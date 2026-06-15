import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  routerPush: vi.fn(),
  pathname: "/alpha/more/issues",
  triggerProps: [] as Array<Record<string, unknown>>,
  menuItemProps: [] as Array<Record<string, unknown>>,
  contentProps: [] as Array<Record<string, unknown>>,
  workspaceData: [
    { id: "ws-1", slug: "alpha", name: "Alpha", avatar_url: null },
    { id: "ws-2", slug: "beta", name: "Beta", avatar_url: null },
  ] as Array<Record<string, unknown>>,
}));

vi.mock("react-native", () => ({
  Image: (props: Record<string, unknown>) => React.createElement("Image", props),
  Pressable: (props: Record<string, unknown>) => React.createElement("Pressable", props, props.children),
  View: (props: Record<string, unknown>) => React.createElement("View", props, props.children),
}));

vi.mock("expo-router", () => ({
  router: { push: mocks.routerPush },
  usePathname: () => mocks.pathname,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mocks.workspaceData }),
}));

vi.mock("react-native-safe-area-context", () => ({
  useSafeAreaInsets: () => ({ bottom: 12 }),
}));

vi.mock("@/components/ui/dropdown-menu", () => ({
  DropdownMenu: (props: Record<string, unknown>) => React.createElement("DropdownMenu", props, props.children),
  DropdownMenuTrigger: (props: Record<string, unknown>) => {
    mocks.triggerProps.push(props);
    return React.createElement("DropdownMenuTrigger", props, props.children);
  },
  DropdownMenuContent: (props: Record<string, unknown>) => {
    mocks.contentProps.push(props);
    return React.createElement("DropdownMenuContent", props, props.children);
  },
  DropdownMenuItem: (props: Record<string, unknown>) => {
    mocks.menuItemProps.push(props);
    return React.createElement("DropdownMenuItem", props, props.children);
  },
  DropdownMenuSeparator: () => React.createElement("DropdownMenuSeparator"),
}));

vi.mock("@/components/ui/text", () => ({
  Text: (props: Record<string, unknown>) => React.createElement("Text", props, props.children),
}));

vi.mock("@/components/workspace/workspace-avatar", () => ({
  WorkspaceAvatar: (props: Record<string, unknown>) => React.createElement("WorkspaceAvatar", props),
}));

vi.mock("@/components/ui/platform-symbol", () => ({
  PlatformSymbol: (props: Record<string, unknown>) => React.createElement("PlatformSymbol", props),
}));

vi.mock("@/data/queries/workspaces", () => ({
  workspaceListOptions: () => ({ kind: "workspaces" }),
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: (selector: (state: { user: { name: string; email: string; avatar_url: null } }) => unknown) =>
    selector({ user: { name: "Alex", email: "alex@example.com", avatar_url: null } }),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceSlug: string }) => unknown) =>
    selector({ currentWorkspaceSlug: "alpha" }),
}));

vi.mock("@/lib/use-color-scheme", () => ({
  useColorScheme: () => ({ colorScheme: "light" }),
}));

vi.mock("@/lib/theme", () => ({
  THEME: {
    light: {
      foreground: "#111",
      mutedForeground: "#999",
    },
  },
}));

vi.mock("@/lib/utils", () => ({
  cn: (...values: Array<string | false | undefined>) =>
    values.filter(Boolean).join(" "),
}));

import { MoreTabDropdownAnchor } from "./more-tab-dropdown";

describe("MoreTabDropdownAnchor", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.routerPush.mockReset();
    mocks.triggerProps.length = 0;
    mocks.menuItemProps.length = 0;
    mocks.contentProps.length = 0;
    mocks.workspaceData = [
      { id: "ws-1", slug: "alpha", name: "Alpha", avatar_url: null },
      { id: "ws-2", slug: "beta", name: "Beta", avatar_url: null },
    ];
    mocks.pathname = "/alpha/more/issues";
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("renders the menu and routes account/workspace/nav presses", () => {
    act(() => {
      renderer = create(
        <MoreTabDropdownAnchor
          triggerRef={{ current: null }}
        />,
      );
    });

    expect(mocks.triggerProps).toHaveLength(1);
    expect(mocks.contentProps[0]).toMatchObject({
      side: "top",
      align: "end",
      sideOffset: 6,
    });

    expect(mocks.menuItemProps).toHaveLength(5);

    act(() => {
      (mocks.menuItemProps[0] as { onPress: () => void }).onPress();
      (mocks.menuItemProps[1] as { onPress: () => void }).onPress();
      (mocks.menuItemProps[2] as { onPress: () => void }).onPress();
      (mocks.menuItemProps[3] as { onPress: () => void }).onPress();
      (mocks.menuItemProps[4] as { onPress: () => void }).onPress();
    });

    expect(mocks.routerPush).toHaveBeenNthCalledWith(1, "/alpha/more/settings");
    expect(mocks.routerPush).toHaveBeenNthCalledWith(2, "/alpha/switch-workspace");
    expect(mocks.routerPush).toHaveBeenNthCalledWith(3, "/alpha/more/pins");
    expect(mocks.routerPush).toHaveBeenNthCalledWith(4, "/alpha/more/issues");
    expect(mocks.routerPush).toHaveBeenNthCalledWith(5, "/alpha/more/projects");
    expect(mocks.menuItemProps[3]?.className).toContain("bg-secondary");
  });

  it("disables workspace switching when there is only one workspace", () => {
    mocks.workspaceData = [
      { id: "ws-1", slug: "alpha", name: "Alpha", avatar_url: null },
    ];

    act(() => {
      renderer = create(
        <MoreTabDropdownAnchor
          triggerRef={{ current: null }}
        />,
      );
    });

    expect(mocks.menuItemProps[1]).toMatchObject({
      disabled: true,
      accessibilityLabel: "Alpha",
    });
  });
});
