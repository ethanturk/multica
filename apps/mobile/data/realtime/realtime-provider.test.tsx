import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create } from "react-test-renderer";
import React from "react";

let useWSClientHook: null | (() => unknown) = null;

const wsInstances: MockWSClient[] = [];
const listeners = {
  appState: null as ((status: "active" | "background" | "inactive") => void) | null,
  netInfo: null as ((state: { isConnected: boolean | null }) => void) | null,
};

const authState = { user: { id: "user-1" } as { id: string } | null };
const workspaceState = { currentWorkspaceSlug: "workspace-a" as string | null };
const getToken = vi.fn(async () => "token-123");
const appStateRemove = vi.fn();
const netInfoUnsub = vi.fn();

class MockWSClient {
  connect = vi.fn();
  resume = vi.fn();
  forceReconnect = vi.fn();
  pause = vi.fn();
  disconnect = vi.fn();

  readonly opts: Record<string, unknown>;

  constructor(opts: Record<string, unknown>) {
    this.opts = opts;
    wsInstances.push(this);
  }
}

vi.mock("react-native", () => ({
  AppState: {
    addEventListener: vi.fn((_event: string, callback: (status: "active" | "background" | "inactive") => void) => {
      listeners.appState = callback;
      return { remove: appStateRemove };
    }),
  },
  Platform: {
    OS: "android",
  },
}));

vi.mock("@react-native-community/netinfo", () => ({
  default: {
    addEventListener: vi.fn((callback: (state: { isConnected: boolean | null }) => void) => {
      listeners.netInfo = callback;
      return netInfoUnsub;
    }),
  },
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: vi.fn((selector: (state: typeof authState) => unknown) => selector(authState)),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: vi.fn((selector: (state: typeof workspaceState) => unknown) => selector(workspaceState)),
}));

vi.mock("@/data/secure-storage", () => ({
  getToken,
}));

vi.mock("./ws-client", () => ({
  WSClient: MockWSClient,
}));

describe("RealtimeProvider", () => {
  const originalApiUrl = process.env.EXPO_PUBLIC_API_URL;

  function HookProbe({ onValue }: { onValue: (value: unknown) => void }) {
    onValue(useWSClientHook?.() ?? null);
    return null;
  }

  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = "http://api.multica.ai";
    vi.resetModules();
    vi.clearAllMocks();
    wsInstances.length = 0;
    listeners.appState = null;
    listeners.netInfo = null;
    authState.user = { id: "user-1" };
    workspaceState.currentWorkspaceSlug = "workspace-a";
    getToken.mockResolvedValue("token-123");
  });

  afterEach(() => {
    if (originalApiUrl === undefined) {
      delete process.env.EXPO_PUBLIC_API_URL;
    } else {
      process.env.EXPO_PUBLIC_API_URL = originalApiUrl;
    }
  });

  it("creates an Android websocket client and wires lifecycle listeners", async () => {
    const { RealtimeProvider, useWSClient } = await import("./realtime-provider");
    useWSClientHook = useWSClient;
    const values: unknown[] = [];

    let renderer: { unmount(): void } | null = null;
    await act(async () => {
      renderer = create(
        <RealtimeProvider>
          <HookProbe onValue={(value) => values.push(value)} />
          <></>
        </RealtimeProvider>,
      );
      await Promise.resolve();
    });

    expect(getToken).toHaveBeenCalledOnce();
    expect(wsInstances).toHaveLength(1);
    expect(wsInstances[0]?.opts).toMatchObject({
      url: "ws://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "workspace-a",
      clientOs: "android",
      clientVersion: "0.1.0",
      logger: console,
    });
    expect(wsInstances[0]?.connect).toHaveBeenCalledOnce();
    expect(values.length).toBeGreaterThan(0);

    listeners.appState?.("background");
    listeners.appState?.("active");
    listeners.appState?.("inactive");
    listeners.netInfo?.({ isConnected: false });
    listeners.netInfo?.({ isConnected: true });

    expect(wsInstances[0]?.pause).toHaveBeenCalledOnce();
    expect(wsInstances[0]?.resume).toHaveBeenCalledOnce();
    expect(wsInstances[0]?.forceReconnect).toHaveBeenCalledTimes(2);

    await act(async () => {
      renderer?.unmount();
    });

    expect(appStateRemove).toHaveBeenCalledOnce();
    expect(netInfoUnsub).toHaveBeenCalledOnce();
    expect(wsInstances[0]?.disconnect).toHaveBeenCalledOnce();
  });

  it("skips client creation when auth, workspace, or token are missing", async () => {
    const { RealtimeProvider, useWSClient } = await import("./realtime-provider");
    useWSClientHook = useWSClient;
    const values: unknown[] = [];

    let renderer: { update(element: React.ReactElement): void; unmount(): void } | null = null;
    await act(async () => {
      renderer = create(
        <RealtimeProvider>
          <HookProbe onValue={(value) => values.push(value)} />
          <></>
        </RealtimeProvider>,
      );
      await Promise.resolve();
    });
    expect(wsInstances).toHaveLength(1);

    authState.user = null;
    await act(async () => {
      renderer?.update(
        <RealtimeProvider>
          <HookProbe onValue={(value) => values.push(value)} />
          <></>
        </RealtimeProvider>,
      );
      await Promise.resolve();
    });
    expect(values.at(-1)).toBeNull();

    authState.user = { id: "user-1" };
    workspaceState.currentWorkspaceSlug = null;
    await act(async () => {
      renderer?.update(
        <RealtimeProvider>
          <HookProbe onValue={(value) => values.push(value)} />
          <></>
        </RealtimeProvider>,
      );
      await Promise.resolve();
    });
    expect(values.at(-1)).toBeNull();

    workspaceState.currentWorkspaceSlug = "workspace-a";
    getToken.mockResolvedValueOnce(null as never);
    await act(async () => {
      renderer?.update(
        <RealtimeProvider>
          <HookProbe onValue={(value) => values.push(value)} />
          <></>
        </RealtimeProvider>,
      );
      await Promise.resolve();
    });
    expect(wsInstances).toHaveLength(1);

    await act(async () => {
      renderer?.unmount();
    });
  });

  it("bails out cleanly when the async token read resolves after unmount", async () => {
    const { RealtimeProvider, useWSClient } = await import("./realtime-provider");
    useWSClientHook = useWSClient;

    let resolveToken: ((value: string) => void) | null = null;
    getToken.mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          resolveToken = resolve;
        }),
    );

    let renderer: { unmount(): void } | null = null;
    await act(async () => {
      renderer = create(
        <RealtimeProvider>
          <HookProbe onValue={() => {}} />
          <></>
        </RealtimeProvider>,
      );
      await Promise.resolve();
    });

    await act(async () => {
      renderer?.unmount();
      await Promise.resolve();
    });

    await act(async () => {
      resolveToken?.("late-token");
      await Promise.resolve();
    });

    expect(wsInstances).toHaveLength(0);
  });
});
