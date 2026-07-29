import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  type ActionSheetRuntime,
  buildAndroidActionSheetPages,
  showPlatformActionSheet,
  showPlatformActionSheetWithRuntime,
} from "./action-sheet";

const iosShowActionSheetWithOptions = vi.fn();
const androidAlert = vi.fn();
const platform = { OS: "android" as "android" | "ios" };

const runtime: ActionSheetRuntime = {
  ActionSheetIOS: {
    showActionSheetWithOptions: iosShowActionSheetWithOptions,
  },
  Alert: {
    alert: androidAlert,
  },
  Platform: platform,
};

describe("buildAndroidActionSheetPages", () => {
  beforeEach(() => {
    iosShowActionSheetWithOptions.mockReset();
    androidAlert.mockReset();
    platform.OS = "android";
  });

  it("keeps short menus on a single alert page", () => {
    const pages = buildAndroidActionSheetPages([
      { label: "Edit" },
      { label: "Delete", style: "destructive" },
      { label: "Cancel", style: "cancel" },
    ]);

    expect(pages).toHaveLength(1);
    expect(pages[0]?.buttons.map((button) => button.label)).toEqual([
      "Edit",
      "Delete",
      "Cancel",
    ]);
  });

  it("paginates longer menus and appends More before the final page", () => {
    const pages = buildAndroidActionSheetPages([
      { label: "Reply" },
      { label: "React…" },
      { label: "Copy" },
      { label: "Select Text" },
      { label: "Cancel", style: "cancel" },
    ]);

    expect(pages).toHaveLength(2);
    expect(pages[0]?.buttons.map((button) => button.label)).toEqual([
      "Reply",
      "React…",
      "More",
    ]);
    expect(pages[1]?.buttons.map((button) => button.label)).toEqual([
      "Copy",
      "Select Text",
      "Cancel",
    ]);
  });

  it("builds a fallback page when cancel is the only option", () => {
    const onCancel = vi.fn();
    const pages = buildAndroidActionSheetPages([
      { label: "Cancel", style: "cancel", onPress: onCancel },
    ]);

    expect(pages).toHaveLength(1);
    expect(pages[0]?.buttons.map((button) => button.label)).toEqual([
      "Cancel",
    ]);
    expect(pages[0]?.buttons[0]).toMatchObject({
      label: "Cancel",
      style: "cancel",
      onPress: onCancel,
    });
  });

  it("builds an OK fallback page when no options are provided", () => {
    const pages = buildAndroidActionSheetPages([]);

    expect(pages).toEqual([
      {
        buttons: [{ label: "OK", style: "cancel" }],
      },
    ]);
  });
});

describe("showPlatformActionSheet", () => {
  beforeEach(() => {
    iosShowActionSheetWithOptions.mockReset();
    androidAlert.mockReset();
  });

  it("uses ActionSheetIOS on ios", () => {
    platform.OS = "ios";
    const onDismiss = vi.fn();
    const onDelete = vi.fn();

    iosShowActionSheetWithOptions.mockImplementation(
      (_config, callback: (index: number) => void) => {
        callback(1);
      },
    );

    showPlatformActionSheetWithRuntime(runtime, {
      title: "Issue",
      options: [
        { label: "Edit" },
        { label: "Delete", style: "destructive", onPress: onDelete },
        { label: "Cancel", style: "cancel" },
      ],
      onDismiss,
    });

    expect(iosShowActionSheetWithOptions).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Issue",
        options: ["Edit", "Delete", "Cancel"],
        cancelButtonIndex: 2,
        destructiveButtonIndex: 1,
      }),
      expect.any(Function),
    );
    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("omits iOS special indices when the menu has no cancel or destructive actions", () => {
    platform.OS = "ios";

    iosShowActionSheetWithOptions.mockImplementation(
      (_config, callback: (index: number) => void) => {
        callback(0);
      },
    );

    showPlatformActionSheetWithRuntime(runtime, {
      title: "Inbox",
      options: [{ label: "Refresh" }, { label: "Mark all read" }],
    });

    expect(iosShowActionSheetWithOptions).toHaveBeenCalledWith(
      {
        title: "Inbox",
        message: undefined,
        options: ["Refresh", "Mark all read"],
      },
      expect.any(Function),
    );
  });

  it("uses Alert.alert on android and executes the tapped action", () => {
    platform.OS = "android";
    const onDismiss = vi.fn();
    const onEdit = vi.fn();

    androidAlert.mockImplementation(
      (_title, _message, buttons: Array<{ onPress?: () => void }>, options) => {
        buttons[0]?.onPress?.();
        options?.onDismiss?.();
      },
    );

    showPlatformActionSheetWithRuntime(runtime, {
      title: "Project",
      options: [
        { label: "Edit", onPress: onEdit },
        { label: "Cancel", style: "cancel" },
      ],
      onDismiss,
    });

    expect(androidAlert).toHaveBeenCalledWith(
      "Project",
      undefined,
      expect.arrayContaining([
        expect.objectContaining({ text: "Edit" }),
        expect.objectContaining({ text: "Cancel" }),
      ]),
      expect.objectContaining({ cancelable: true, onDismiss }),
    );
    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(2);
  });

  it("opens the next android page when More is tapped", () => {
    platform.OS = "android";
    const onDismiss = vi.fn();

    androidAlert
      .mockImplementationOnce((_title, _message, buttons) => {
        buttons[2]?.onPress?.();
      })
      .mockImplementationOnce((_title, _message, buttons) => {
        buttons[0]?.onPress?.();
      });

    const onCopy = vi.fn();

    showPlatformActionSheetWithRuntime(runtime, {
      options: [
        { label: "Reply" },
        { label: "React…" },
        { label: "Copy", onPress: onCopy },
        { label: "Select Text" },
        { label: "Cancel", style: "cancel" },
      ],
      onDismiss,
    });

    expect(androidAlert).toHaveBeenCalledTimes(2);
    expect(onCopy).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("renders an Android OK fallback when the menu is empty", () => {
    platform.OS = "android";

    showPlatformActionSheetWithRuntime(runtime, {
      title: "Empty",
      options: [],
    });

    expect(androidAlert).toHaveBeenCalledWith(
      "Empty",
      undefined,
      [expect.objectContaining({ text: "OK", style: "cancel" })],
      expect.objectContaining({ cancelable: true }),
    );
  });

  it("supports the public wrapper with an injected runtime", () => {
    platform.OS = "android";
    const onDismiss = vi.fn();

    androidAlert.mockImplementation(
      (_title, _message, buttons: Array<{ onPress?: () => void }>, options) => {
        buttons[1]?.onPress?.();
        options?.onDismiss?.();
      },
    );

    showPlatformActionSheet(
      {
        title: "Inbox",
        options: [
          { label: "Mark all read" },
          { label: "Cancel", style: "cancel" },
        ],
        onDismiss,
      },
      runtime,
    );

    expect(androidAlert).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(2);
  });

});
