import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create } from "react-test-renderer";

import { PlatformSymbol } from "./platform-symbol";

const mocks = vi.hoisted(() => ({
  platform: { OS: "ios" },
  imageProps: [] as Record<string, unknown>[],
  iconProps: [] as Record<string, unknown>[],
}));

vi.mock("react-native", () => ({
  Platform: mocks.platform,
}));

vi.mock("expo-image", () => ({
  Image: (props: Record<string, unknown>) => {
    mocks.imageProps.push(props);
    return React.createElement("ExpoImage", props);
  },
}));

vi.mock("@expo/vector-icons", () => ({
  Ionicons: (props: Record<string, unknown>) => {
    mocks.iconProps.push(props);
    return React.createElement("Ionicons", props);
  },
}));

describe("PlatformSymbol", () => {
  beforeEach(() => {
    // react-test-renderer on React 19 requires the act environment flag.
     
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.imageProps.length = 0;
    mocks.iconProps.length = 0;
  });

  afterEach(() => {
    mocks.platform.OS = "ios";
  });

  it("renders an SF Symbol image on ios", () => {
    mocks.platform.OS = "ios";

    act(() => {
      create(
        <PlatformSymbol
          name="tray.fill"
          size={18}
          tintColor="#123456"
        />,
      );
    });

    expect(mocks.imageProps).toHaveLength(1);
    expect(mocks.imageProps[0]).toMatchObject({
      source: "sf:tray.fill",
      tintColor: "#123456",
    });
    expect(mocks.iconProps).toHaveLength(0);
  });

  it("renders the Android icon mapping outside ios", () => {
    mocks.platform.OS = "android";

    act(() => {
      create(
        <PlatformSymbol
          name="chevron.right"
          size={12}
          tintColor="#654321"
        />,
      );
    });

    expect(mocks.iconProps).toHaveLength(1);
    expect(mocks.iconProps[0]).toMatchObject({
      name: "chevron-forward",
      size: 12,
      color: "#654321",
    });
    expect(mocks.imageProps).toHaveLength(0);
  });
});
