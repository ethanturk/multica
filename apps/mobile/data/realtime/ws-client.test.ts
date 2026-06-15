import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { WSClient } from "./ws-client";

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  readyState = FakeWebSocket.OPEN;
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

  it("adds workspace metadata to the websocket URL and auths on open", () => {
    const client = new WSClient({
      url: "wss://api.multica.ai/ws",
      token: "token-123",
      workspaceSlug: "android-team",
      clientVersion: "0.1.0",
    });

    client.connect();

    expect(FakeWebSocket.instances).toHaveLength(1);
    const socket = FakeWebSocket.instances[0];
    const url = new URL(socket.url);

    expect(url.origin + url.pathname).toBe("wss://api.multica.ai/ws");
    expect(url.searchParams.get("workspace_slug")).toBe("android-team");
    expect(url.searchParams.get("client_platform")).toBe("mobile");
    expect(url.searchParams.get("client_os")).toBe("ios");
    expect(url.searchParams.get("client_version")).toBe("0.1.0");

    socket.emitOpen();

    expect(socket.sent).toEqual([
      JSON.stringify({ type: "auth", payload: { token: "token-123" } }),
    ]);
  });

  it("defaults the websocket client_os to ios", () => {
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
    });

    client.connect();
    FakeWebSocket.instances[0].emitClose();

    expect(FakeWebSocket.instances).toHaveLength(1);

    vi.advanceTimersByTime(1000);

    expect(FakeWebSocket.instances).toHaveLength(2);
  });
});
