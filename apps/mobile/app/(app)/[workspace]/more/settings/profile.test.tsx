import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

import ProfileSettingsScreen from "./profile";

const mocks = vi.hoisted(() => ({
  alertMock: vi.fn(),
  requestCameraPermissionsAsync: vi.fn(),
  launchCameraAsync: vi.fn(),
  launchImageLibraryAsync: vi.fn(),
  updateMe: vi.fn(),
  uploadFile: vi.fn(),
  showPlatformActionSheet: vi.fn(),
  setUser: vi.fn(),
  user: {
    id: "member-1",
    name: "Alice Example",
    email: "alice@example.com",
    avatar_url: "",
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

vi.mock("expo-image-picker", () => ({
  requestCameraPermissionsAsync: mocks.requestCameraPermissionsAsync,
  launchCameraAsync: mocks.launchCameraAsync,
  launchImageLibraryAsync: mocks.launchImageLibraryAsync,
}));

vi.mock("@/components/ui/text", () => ({
  Text: (props: Record<string, any>) => React.createElement("Text", props, props.children),
}));

vi.mock("@/components/ui/button", () => ({
  Button: (props: Record<string, any>) => React.createElement("Button", props, props.children),
}));

vi.mock("@/components/ui/text-field", () => ({
  TextField: (props: Record<string, any>) => React.createElement("TextField", props),
}));

vi.mock("@/components/ui/avatar", () => ({
  Avatar: (props: Record<string, any>) => React.createElement("Avatar", props, props.children),
  AvatarFallback: (props: Record<string, any>) =>
    React.createElement("AvatarFallback", props, props.children),
  AvatarImage: (props: Record<string, any>) => React.createElement("AvatarImage", props),
}));

vi.mock("@/components/ui/separator", () => ({
  Separator: (props: Record<string, any>) => React.createElement("Separator", props),
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: (
    selector: (state: { user: typeof mocks.user; setUser: typeof mocks.setUser }) => unknown,
  ) => selector({ user: mocks.user, setUser: mocks.setUser }),
}));

vi.mock("@/data/api", () => ({
  api: {
    updateMe: mocks.updateMe,
    uploadFile: mocks.uploadFile,
  },
}));

vi.mock("@/lib/action-sheet", () => ({
  showPlatformActionSheet: mocks.showPlatformActionSheet,
}));

describe("ProfileSettingsScreen", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
     
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.alertMock.mockReset();
    mocks.requestCameraPermissionsAsync.mockReset();
    mocks.launchCameraAsync.mockReset();
    mocks.launchImageLibraryAsync.mockReset();
    mocks.updateMe.mockReset();
    mocks.uploadFile.mockReset();
    mocks.showPlatformActionSheet.mockReset();
    mocks.setUser.mockReset();
    mocks.user = {
      id: "member-1",
      name: "Alice Example",
      email: "alice@example.com",
      avatar_url: "",
    };
    vi.spyOn(Date, "now").mockReturnValue(123);
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
    vi.restoreAllMocks();
  });

  it("resyncs the form when the stored user changes and ignores no-op saves", () => {
    act(() => {
      renderer = create(<ProfileSettingsScreen />);
    });

    let textField = renderer!.root.findByType("TextField");
    expect(textField.props.value).toBe("Alice Example");

    act(() => {
      textField.props.onChangeText("Alice Example");
      renderer!.root.findByType("Button").props.onPress();
    });
    expect(mocks.updateMe).not.toHaveBeenCalled();

    act(() => {
      mocks.user = { ...mocks.user, name: "Bob Stone" };
      renderer!.update(<ProfileSettingsScreen />);
    });

    textField = renderer!.root.findByType("TextField");
    expect(textField.props.value).toBe("Bob Stone");
  });

  it("saves the trimmed display name", async () => {
    mocks.updateMe.mockResolvedValue({
      ...mocks.user,
      name: "Alice Updated",
    });

    await act(async () => {
      renderer = create(<ProfileSettingsScreen />);
    });

    const textField = renderer!.root.findByType("TextField");
    act(() => {
      textField.props.onChangeText("  Alice Updated  ");
    });

    await act(async () => {
      await renderer!.root.findByType("Button").props.onPress();
    });

    expect(mocks.updateMe).toHaveBeenCalledWith({ name: "Alice Updated" });
    expect(mocks.setUser).toHaveBeenCalledWith(
      expect.objectContaining({ name: "Alice Updated" }),
    );
  });

  it("handles avatar action sheet options, permission denial, uploads, and removal", async () => {
    mocks.user = {
      ...mocks.user,
      avatar_url: "https://cdn.example/avatar.png",
    };
    mocks.requestCameraPermissionsAsync.mockResolvedValue({ granted: false });
    mocks.launchImageLibraryAsync.mockResolvedValue({
      canceled: false,
      assets: [
        {
          uri: "file:///tmp/avatar.png",
          fileSize: 2048,
          fileName: undefined,
          mimeType: undefined,
        },
      ],
    });
    mocks.uploadFile.mockResolvedValue({
      url: "https://cdn.example/uploaded.png",
    });
    mocks.updateMe
      .mockResolvedValueOnce({
        ...mocks.user,
        avatar_url: "https://cdn.example/uploaded.png",
      })
      .mockResolvedValueOnce({
        ...mocks.user,
        avatar_url: "",
      });

    await act(async () => {
      renderer = create(<ProfileSettingsScreen />);
    });

    act(() => {
      renderer!.root.findByType("Pressable").props.onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Take Photo",
      "Choose from Library",
      "Remove Photo",
      "Cancel",
    ]);

    await act(async () => {
      await config.options[0].onPress();
    });
    expect(mocks.alertMock).toHaveBeenCalledWith(
      "Permission needed",
      "Camera access is required to take a photo.",
    );

    await act(async () => {
      await config.options[1].onPress();
    });
    expect(mocks.uploadFile).toHaveBeenCalledWith({
      uri: "file:///tmp/avatar.png",
      name: "avatar-123.jpg",
      type: "image/jpeg",
    });
    expect(mocks.updateMe).toHaveBeenCalledWith({
      avatar_url: "https://cdn.example/uploaded.png",
    });

    await act(async () => {
      await config.options[2].onPress();
    });
    expect(mocks.updateMe).toHaveBeenCalledWith({ avatar_url: "" });
  });

  it("alerts on oversized library images and failed saves", async () => {
    mocks.launchImageLibraryAsync.mockResolvedValue({
      canceled: false,
      assets: [
        {
          uri: "file:///tmp/huge.png",
          fileSize: 6 * 1024 * 1024,
          fileName: "huge.png",
          mimeType: "image/png",
        },
      ],
    });
    mocks.updateMe.mockRejectedValueOnce(new Error("save exploded"));

    await act(async () => {
      renderer = create(<ProfileSettingsScreen />);
    });

    act(() => {
      renderer!.root.findByType("Pressable").props.onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    await act(async () => {
      await config.options[1].onPress();
    });
    expect(mocks.alertMock).toHaveBeenCalledWith(
      "Image too large",
      "Pick an image under 5 MB.",
    );

    const textField = renderer!.root.findByType("TextField");
    act(() => {
      textField.props.onChangeText("Alice Broken");
    });
    await act(async () => {
      await renderer!.root.findByType("Button").props.onPress();
    });
    expect(mocks.alertMock).toHaveBeenCalledWith(
      "Save failed",
      "save exploded",
    );
  });

  it("alerts when avatar upload or removal fails", async () => {
    mocks.user = {
      ...mocks.user,
      avatar_url: "https://cdn.example/avatar.png",
    };
    mocks.launchCameraAsync.mockResolvedValue({
      canceled: false,
      assets: [
        {
          uri: "file:///tmp/fail.jpg",
          fileSize: 2048,
          fileName: "fail.jpg",
          mimeType: "image/jpeg",
        },
      ],
    });
    mocks.requestCameraPermissionsAsync.mockResolvedValue({ granted: true });
    mocks.uploadFile.mockRejectedValueOnce(new Error("upload exploded"));
    mocks.updateMe.mockRejectedValueOnce(new Error("remove exploded"));

    await act(async () => {
      renderer = create(<ProfileSettingsScreen />);
    });

    act(() => {
      renderer!.root.findByType("Pressable").props.onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    await act(async () => {
      await config.options[0].onPress();
    });
    expect(mocks.alertMock).toHaveBeenCalledWith(
      "Upload failed",
      "upload exploded",
    );

    await act(async () => {
      await config.options[2].onPress();
    });
    expect(mocks.alertMock).toHaveBeenCalledWith(
      "Remove failed",
      "remove exploded",
    );
  });
});
