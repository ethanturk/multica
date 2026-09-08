import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readyState = MockWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  readonly sent: string[] = [];

  constructor(readonly url: string) {
    MockWebSocket.instances.push(this);
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  receive(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  send(frame: string) {
    this.sent.push(frame);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }
}

function connectAuthenticatedClient() {
  const client = new WSClient({
    url: "wss://example.test/ws",
    token: "token",
    workspaceSlug: "workspace",
  });
  client.connect();
  const socket = MockWebSocket.instances[0];
  socket.open();
  socket.receive({ type: "auth_ack" });
  return { client, socket };
}

describe("WSClient application heartbeat", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("reconnects a stale OPEN socket through the jittered backoff path", () => {
    vi.spyOn(Math, "random").mockReturnValue(0);
    const { client, socket } = connectAuthenticatedClient();

    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "auth", payload: { token: "token" } },
      { type: "ping" },
    ]);

    // No pong arrives even though the JS-visible readyState remains OPEN.
    vi.advanceTimersByTime(10_000);
    vi.advanceTimersByTime(1);

    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("keeps a healthy socket connected when its pong arrives", () => {
    const { client, socket } = connectAuthenticatedClient();
    socket.receive({ type: "pong" });

    vi.advanceTimersByTime(10_000);

    expect(MockWebSocket.instances).toHaveLength(1);
    client.disconnect();
  });
});

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  readyState = FakeWebSocket.CONNECTING;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
  }

  emitOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.({} as Event);
  }

  emitMessage(data: string) {
    this.onmessage?.({ data } as MessageEvent);
  }

  emitClose() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({} as CloseEvent);
  }
}

describe("WSClient", () => {
  const originalWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.useFakeTimers();
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  });

  afterEach(() => {
    vi.useRealTimers();
    globalThis.WebSocket = originalWebSocket;
  });

  it("adds workspace and Android client metadata to the websocket URL and auths on open", () => {
    const client = new WSClient({
      url: "wss://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "android-team",
      clientOs: "android",
      clientVersion: "0.1.0",
    });

    client.connect();

    expect(FakeWebSocket.instances).toHaveLength(1);
    const socket = FakeWebSocket.instances[0];
    const url = new URL(socket.url);

    expect(url.origin + url.pathname).toBe("wss://api.multica.ai/ws");
    expect(url.searchParams.get("workspace_slug")).toBe("android-team");
    expect(url.searchParams.get("client_platform")).toBe("mobile");
    expect(url.searchParams.get("client_os")).toBe("android");
    expect(url.searchParams.get("client_version")).toBe("0.1.0");

    socket.emitOpen();

    expect(socket.sent).toEqual([
      JSON.stringify({ type: "auth", payload: { token: "token-123" } }),
    ]);
  });

  it("defaults the websocket client_os to ios when none is provided", () => {
    const client = new WSClient({
      url: "wss://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "ios-team",
    });

    client.connect();

    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(
      new URL(FakeWebSocket.instances[0].url).searchParams.get("client_os"),
    ).toBe("ios");
  });

  it("fires reconnect callbacks only after a later auth_ack", () => {
    const onReconnect = vi.fn();
    const client = new WSClient({
      url: "wss://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "realtime-team",
      clientOs: "android",
    });
    client.onReconnect(onReconnect);

    client.connect();
    const firstSocket = FakeWebSocket.instances[0];
    firstSocket.emitMessage(JSON.stringify({ type: "auth_ack" }));

    expect(onReconnect).not.toHaveBeenCalled();

    client.forceReconnect();

    expect(FakeWebSocket.instances).toHaveLength(2);
    const secondSocket = FakeWebSocket.instances[1];
    secondSocket.emitMessage(JSON.stringify({ type: "auth_ack" }));

    expect(onReconnect).toHaveBeenCalledTimes(1);
  });

  it("reconnects after a close while active", () => {
    vi.spyOn(Math, "random").mockReturnValue(0.5);

    const client = new WSClient({
      url: "wss://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "reconnect-team",
      clientOs: "android",
    });

    client.connect();
    FakeWebSocket.instances[0].emitClose();

    expect(FakeWebSocket.instances).toHaveLength(1);

    vi.advanceTimersByTime(1000);

    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  it("handles message routing, reconnect timers, and teardown edge cases", () => {
    vi.spyOn(Math, "random").mockReturnValue(0.5);

    const logger = {
      info: vi.fn(),
      warn: vi.fn(),
      debug: vi.fn(),
    };
    const onEvent = vi.fn();
    const onAny = vi.fn();
    const onReconnect = vi.fn(() => {
      throw new Error("boom");
    });
    const clearTimeoutSpy = vi.spyOn(globalThis, "clearTimeout");

    const client = new WSClient({
      url: "wss://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "edge-team",
      clientOs: "android",
      logger,
    });

    const removeEvent = client.on("issue:updated", onEvent);
    const removeAny = client.onAny(onAny);
    const removeReconnect = client.onReconnect(onReconnect);

    client.connect();
    const firstSocket = FakeWebSocket.instances[0];

    client.send({ type: "issue:updated", payload: { id: "before-open" } } as never);
    expect(firstSocket?.sent).toHaveLength(0);

    firstSocket?.emitOpen();
    client.send({ type: "issue:updated", payload: { id: "after-open" } } as never);
    expect(firstSocket?.sent.at(-1)).toBe(
      JSON.stringify({ type: "issue:updated", payload: { id: "after-open" } }),
    );

    firstSocket?.emitMessage("not-json");
    firstSocket?.emitMessage(JSON.stringify({ error: "bad frame" }));
    firstSocket?.emitMessage(
      JSON.stringify({
        type: "issue:updated",
        payload: { id: "issue-1" },
        actor_id: "member-1",
      }),
    );
    firstSocket?.emitMessage(JSON.stringify({ type: "auth_ack" }));

    expect(logger.warn).toHaveBeenCalledWith("[ws] non-JSON frame ignored");
    expect(logger.warn).toHaveBeenCalledWith("[ws] frame without type", JSON.stringify({ error: "bad frame" }));
    expect(onEvent).toHaveBeenCalledWith({ id: "issue-1" }, "member-1");
    expect(onAny).toHaveBeenCalledWith({
      type: "issue:updated",
      payload: { id: "issue-1" },
      actor_id: "member-1",
    });

    firstSocket?.emitClose();
    expect(FakeWebSocket.instances).toHaveLength(1);

    client.pause();
    // Both the pending reconnect timer and the heartbeat pong timer are cleared.
    expect(clearTimeoutSpy).toHaveBeenCalledTimes(2);

    vi.advanceTimersByTime(1000);
    expect(FakeWebSocket.instances).toHaveLength(1);

    client.resume();
    expect(FakeWebSocket.instances).toHaveLength(2);
    const secondSocket = FakeWebSocket.instances[1];
    secondSocket?.emitMessage(JSON.stringify({ type: "auth_ack" }));
    expect(logger.warn).toHaveBeenCalledWith("[ws] onReconnect callback threw", expect.any(Error));

    removeReconnect();
    client.forceReconnect();
    expect(FakeWebSocket.instances).toHaveLength(3);
    const thirdSocket = FakeWebSocket.instances[2];
    thirdSocket.close = vi.fn(() => {
      throw new Error("already closed");
    });
    thirdSocket?.emitClose();
    vi.advanceTimersByTime(1000);
    expect(FakeWebSocket.instances).toHaveLength(4);

    removeEvent();
    removeAny();
    thirdSocket?.emitMessage(
      JSON.stringify({
        type: "issue:updated",
        payload: { id: "issue-2" },
        actor_id: "member-2",
      }),
    );
    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(onAny).toHaveBeenCalledTimes(1);

    client.disconnect();
    expect(logger.debug).toHaveBeenCalledWith("[ws] event", "issue:updated");
  });
});
