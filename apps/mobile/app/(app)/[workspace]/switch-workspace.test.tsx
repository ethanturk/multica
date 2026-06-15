import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  alertMock: vi.fn(),
  dismiss: vi.fn(),
  replace: vi.fn(),
  currentWorkspaceSlug: "alpha",
  queryState: {
    data: undefined as
      | Array<{ id: string; slug: string; name: string; avatar_url: string | null }>
      | undefined,
    isLoading: false,
  },
}));

vi.mock("react-native", () => ({
  ActivityIndicator: (props: Record<string, any>) =>
    React.createElement("ActivityIndicator", props),
  Alert: { alert: mocks.alertMock },
  Pressable: (props: Record<string, any>) =>
    React.createElement("Pressable", props, props.children),
  ScrollView: (props: Record<string, any>) =>
    React.createElement("ScrollView", props, props.children),
  View: (props: Record<string, any>) =>
    React.createElement("View", props, props.children),
}));

vi.mock("expo-router", () => ({
  router: {
    dismiss: mocks.dismiss,
    replace: mocks.replace,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => mocks.queryState,
}));

vi.mock("@/components/ui/platform-symbol", () => ({
  PlatformSymbol: (props: Record<string, any>) =>
    React.createElement("PlatformSymbol", props),
}));

vi.mock("@/components/ui/text", () => ({
  Text: (props: Record<string, any>) => React.createElement("Text", props, props.children),
}));

vi.mock("@/components/workspace/workspace-avatar", () => ({
  WorkspaceAvatar: (props: Record<string, any>) =>
    React.createElement("WorkspaceAvatar", props),
}));

vi.mock("@/data/queries/workspaces", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (state: { currentWorkspaceSlug: string | null }) => unknown,
  ) => selector({ currentWorkspaceSlug: mocks.currentWorkspaceSlug }),
}));

vi.mock("@/lib/use-color-scheme", () => ({
  useColorScheme: () => ({ colorScheme: "light" }),
}));

vi.mock("@/lib/theme", () => ({
  THEME: {
    light: {
      foreground: "#111111",
    },
  },
}));

vi.mock("@/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) =>
    values.filter(Boolean).join(" "),
}));

import SwitchWorkspaceRoute from "./switch-workspace";

describe("SwitchWorkspaceRoute", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.alertMock.mockReset();
    mocks.dismiss.mockReset();
    mocks.replace.mockReset();
    mocks.currentWorkspaceSlug = "alpha";
    mocks.queryState.data = undefined;
    mocks.queryState.isLoading = false;
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("renders the loading state", () => {
    mocks.queryState.isLoading = true;

    act(() => {
      renderer = create(<SwitchWorkspaceRoute />);
    });

    expect(renderer!.root.findAllByType("ActivityIndicator")).toHaveLength(1);
  });

  it("renders workspace rows and confirms before switching", () => {
    mocks.queryState.data = [
      { id: "ws-1", slug: "alpha", name: "Alpha", avatar_url: null },
      { id: "ws-2", slug: "beta", name: "Beta", avatar_url: "https://cdn/beta.png" },
    ];

    act(() => {
      renderer = create(<SwitchWorkspaceRoute />);
    });

    const pressables = renderer!.root.findAllByType("Pressable");
    expect(pressables).toHaveLength(2);
    expect(pressables[0]?.props.disabled).toBe(true);
    expect(pressables[1]?.props.disabled).toBe(false);
    expect(renderer!.root.findAllByType("PlatformSymbol")).toHaveLength(1);

    act(() => {
      pressables[0]?.props.onPress();
    });
    expect(mocks.alertMock).not.toHaveBeenCalled();

    act(() => {
      pressables[1]?.props.onPress();
    });
    expect(mocks.alertMock).toHaveBeenCalledWith(
      "Switch workspace",
      'Switch to "Beta"?',
      expect.any(Array),
    );

    const buttons = mocks.alertMock.mock.calls[0]?.[2];
    act(() => {
      buttons[1].onPress();
    });

    expect(mocks.dismiss).toHaveBeenCalledTimes(1);
    expect(mocks.replace).toHaveBeenCalledWith("/beta/inbox");
  });
});
