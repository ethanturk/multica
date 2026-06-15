import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create } from "react-test-renderer";

import Index from "./index";

const mocks = vi.hoisted(() => ({
  route: null as string | null,
  authUser: null as { id: string } | null,
  authLoading: true,
  workspaceSlug: null as string | null,
}));

vi.mock("react-native", () => ({
  ActivityIndicator: () => React.createElement("ActivityIndicator"),
  View: ({ children, ...props }: { children?: React.ReactNode } & Record<string, unknown>) =>
    React.createElement("View", props, children),
}));

vi.mock("expo-router", () => ({
  Redirect: ({ href }: { href: string }) => React.createElement("Redirect", { href }),
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: (
    selector: (state: { user: { id: string } | null; isLoading: boolean }) => unknown,
  ) =>
    selector({
      user: mocks.authUser,
      isLoading: mocks.authLoading,
    }),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (state: { currentWorkspaceSlug: string | null }) => unknown,
  ) =>
    selector({
      currentWorkspaceSlug: mocks.workspaceSlug,
    }),
}));

vi.mock("@/lib/entry-route", () => ({
  getEntryRoute: vi.fn(() => mocks.route),
}));

const { getEntryRoute } = await import("@/lib/entry-route");

describe("Index", () => {
  beforeEach(() => {
    // react-test-renderer on React 19 requires the act environment flag.

    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.route = null;
    mocks.authUser = null;
    mocks.authLoading = true;
    mocks.workspaceSlug = null;
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("renders the loading state while the entry route is unresolved", async () => {
    let renderer: ReturnType<typeof create> | null = null;

    await act(async () => {
      renderer = create(<Index />);
    });

    expect(renderer).not.toBeNull();
    expect(renderer!.root.findByType("View")).toBeDefined();
    expect(renderer!.root.findByType("ActivityIndicator")).toBeDefined();
    expect(renderer!.root.findAllByType("Redirect")).toHaveLength(0);
    expect(renderer!.root.findByType("View").props.className).toBe(
      "flex-1 items-center justify-center bg-background",
    );

    renderer!.unmount();
  });

  it("passes the current auth and workspace state into the route helper", async () => {
    mocks.route = "/select-workspace";
    mocks.authUser = { id: "user-7" };
    mocks.authLoading = false;
    mocks.workspaceSlug = "beta";

    let renderer: ReturnType<typeof create> | null = null;

    await act(async () => {
      renderer = create(<Index />);
    });

    expect(getEntryRoute).toHaveBeenCalledWith({
      isLoading: false,
      user: { id: "user-7" },
      workspaceSlug: "beta",
    });

    renderer!.unmount();
  });

  it.each([
    "/login",
    "/select-workspace",
    "/alpha/inbox",
  ])("renders a redirect once the entry route resolves to %s", async (route) => {
    mocks.route = route;
    mocks.authUser = { id: "user-1" };
    mocks.authLoading = false;
    mocks.workspaceSlug = route === "/alpha/inbox" ? "alpha" : null;
    let renderer: ReturnType<typeof create> | null = null;

    await act(async () => {
      renderer = create(<Index />);
    });

    expect(renderer).not.toBeNull();
    expect(renderer!.root.findByType("Redirect").props.href).toBe(route);
    expect(renderer!.root.findAllByType("ActivityIndicator")).toHaveLength(0);

    renderer!.unmount();
  });
});
