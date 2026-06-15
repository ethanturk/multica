import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  clipboardSetStringAsync: vi.fn(),
  selectionAsync: vi.fn(() => Promise.resolve()),
  notificationAsync: vi.fn(() => Promise.resolve()),
  setSelecting: vi.fn(),
  showPlatformActionSheet: vi.fn(),
}));

vi.mock("expo-clipboard", () => ({
  setStringAsync: mocks.clipboardSetStringAsync,
}));

vi.mock("expo-haptics", () => ({
  selectionAsync: mocks.selectionAsync,
  notificationAsync: mocks.notificationAsync,
  NotificationFeedbackType: {
    Success: "success",
  },
}));

vi.mock("@/data/chat-select-store", () => ({
  useChatSelectStore: {
    getState: () => ({
      setSelecting: mocks.setSelecting,
    }),
  },
}));

vi.mock("@/lib/action-sheet", () => ({
  showPlatformActionSheet: mocks.showPlatformActionSheet,
}));

import { useChatMessageLongPress } from "./message-long-press";

describe("useChatMessageLongPress", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    mocks.clipboardSetStringAsync.mockReset();
    mocks.selectionAsync.mockClear();
    mocks.notificationAsync.mockClear();
    mocks.setSelecting.mockReset();
    mocks.showPlatformActionSheet.mockReset();
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("shows copy and select actions for messages with content", () => {
    let captured:
      | ReturnType<typeof useChatMessageLongPress>
      | undefined;

    function Harness() {
      captured = useChatMessageLongPress({
        id: "message-1",
        content: "hello world",
      } as never);
      return null;
    }

    act(() => {
      renderer = create(<Harness />);
    });

    act(() => {
      captured?.onLongPress();
    });

    expect(mocks.selectionAsync).toHaveBeenCalledTimes(1);
    expect(captured?.isPressed).toBe(true);
    expect(mocks.showPlatformActionSheet).toHaveBeenCalledTimes(1);

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Copy",
      "Select Text",
      "Cancel",
    ]);

    act(() => {
      config.options[0].onPress();
      config.options[1].onPress();
      config.onDismiss();
    });

    expect(mocks.clipboardSetStringAsync).toHaveBeenCalledWith("hello world");
    expect(mocks.notificationAsync).toHaveBeenCalledWith("success");
    expect(mocks.setSelecting).toHaveBeenCalledWith("message-1");
    expect(captured?.isPressed).toBe(false);
  });

  it("only shows cancel for empty messages", () => {
    let captured:
      | ReturnType<typeof useChatMessageLongPress>
      | undefined;

    function Harness() {
      captured = useChatMessageLongPress({
        id: "message-2",
        content: "",
      } as never);
      return null;
    }

    act(() => {
      renderer = create(<Harness />);
    });

    act(() => {
      captured?.onLongPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Cancel",
    ]);
  });
});
