import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  alertMock: vi.fn(),
  openUrl: vi.fn(),
  routerBack: vi.fn(),
  routerPush: vi.fn(),
  stackScreens: [] as Array<Record<string, any>>,
  iconButtons: [] as Array<Record<string, any>>,
  headerCards: [] as Array<Record<string, any>>,
  propertiesProps: [] as Array<Record<string, any>>,
  resourcesProps: [] as Array<Record<string, any>>,
  relatedIssueProps: [] as Array<Record<string, any>>,
  deleteMutate: vi.fn(),
  createPinMutate: vi.fn(),
  deletePinMutate: vi.fn(),
  invalidateQueries: vi.fn(() => Promise.resolve()),
  showPlatformActionSheet: vi.fn(),
  projectRealtime: vi.fn(),
  queryState: {
    detail: {
      data: undefined as unknown,
      isLoading: false,
      error: null as unknown,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    },
    pins: {
      data: [] as unknown[],
      isLoading: false,
      error: null as unknown,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    },
  },
}));

vi.mock("react-native", () => ({
  ActivityIndicator: (props: Record<string, any>) =>
    React.createElement("ActivityIndicator", props),
  Alert: { alert: mocks.alertMock },
  Linking: { openURL: mocks.openUrl },
  RefreshControl: (props: Record<string, any>) =>
    React.createElement("RefreshControl", props),
  ScrollView: (props: Record<string, any>) => React.createElement("ScrollView", props, props.children),
  View: (props: Record<string, any>) => React.createElement("View", props, props.children),
}));

vi.mock("react-native-safe-area-context", () => ({
  SafeAreaView: (props: Record<string, any>) =>
    React.createElement("SafeAreaView", props, props.children),
}));

vi.mock("expo-router", () => ({
  Stack: {
    Screen: (props: Record<string, any>) => {
      mocks.stackScreens.push(props);
      return React.createElement("StackScreen", props);
    },
  },
  router: {
    back: mocks.routerBack,
    push: mocks.routerPush,
  },
  useLocalSearchParams: () => ({ id: "project-1" }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (arg: { kind: string }) =>
    arg.kind === "detail" ? mocks.queryState.detail : mocks.queryState.pins,
  useQueryClient: () => ({
    invalidateQueries: mocks.invalidateQueries,
  }),
}));

vi.mock("@/components/ui/text", () => ({
  Text: (props: Record<string, any>) => React.createElement("Text", props),
}));

vi.mock("@/components/ui/button", () => ({
  Button: (props: Record<string, any>) => React.createElement("Button", props),
}));

vi.mock("@/components/ui/icon-button", () => ({
  IconButton: (props: Record<string, any>) => {
    mocks.iconButtons.push(props);
    return React.createElement("IconButton", props);
  },
}));

vi.mock("@/components/project/project-header-card", () => ({
  ProjectHeaderCard: (props: Record<string, any>) => {
    mocks.headerCards.push(props);
    return React.createElement("ProjectHeaderCard", props);
  },
}));

vi.mock("@/components/project/project-properties-section", () => ({
  ProjectPropertiesSection: (props: Record<string, any>) => {
    mocks.propertiesProps.push(props);
    return React.createElement("ProjectPropertiesSection", props);
  },
}));

vi.mock("@/components/project/project-related-issues", () => ({
  ProjectRelatedIssues: (props: Record<string, any>) => {
    mocks.relatedIssueProps.push(props);
    return React.createElement("ProjectRelatedIssues", props);
  },
}));

vi.mock("@/components/project/project-resources-section", () => ({
  ProjectResourcesSection: (props: Record<string, any>) => {
    mocks.resourcesProps.push(props);
    return React.createElement("ProjectResourcesSection", props);
  },
}));

vi.mock("@/data/queries/projects", () => ({
  projectDetailOptions: () => ({ kind: "detail" }),
  projectResourcesOptions: () => ({ queryKey: ["project-resources"] }),
}));

vi.mock("@/data/queries/issue-keys", () => ({
  issueKeys: {
    list: (wsId: string) => ["issues", wsId],
  },
}));

vi.mock("@/data/mutations/projects", () => ({
  useDeleteProject: () => ({
    mutate: mocks.deleteMutate,
  }),
}));

vi.mock("@/data/queries/pins", () => ({
  pinListOptions: () => ({ kind: "pins" }),
}));

vi.mock("@/data/mutations/pins", () => ({
  useCreatePin: () => ({ mutate: mocks.createPinMutate }),
  useDeletePin: () => ({ mutate: mocks.deletePinMutate }),
}));

vi.mock("@/data/auth-store", () => ({
  useAuthStore: (selector: (state: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));

vi.mock("@/data/realtime/use-project-realtime", () => ({
  useProjectRealtime: (...args: unknown[]) => mocks.projectRealtime(...args),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (selector: (state: { currentWorkspaceId: string; currentWorkspaceSlug: string }) => unknown) =>
    selector({ currentWorkspaceId: "ws-1", currentWorkspaceSlug: "alpha" }),
}));

vi.mock("@/lib/action-sheet", () => ({
  showPlatformActionSheet: mocks.showPlatformActionSheet,
}));

import ProjectDetail from "./[id]";

describe("ProjectDetail", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.alertMock.mockReset();
    mocks.openUrl.mockReset();
    mocks.routerBack.mockReset();
    mocks.routerPush.mockReset();
    mocks.stackScreens.length = 0;
    mocks.iconButtons.length = 0;
    mocks.headerCards.length = 0;
    mocks.propertiesProps.length = 0;
    mocks.resourcesProps.length = 0;
    mocks.relatedIssueProps.length = 0;
    mocks.deleteMutate.mockReset();
    mocks.createPinMutate.mockReset();
    mocks.deletePinMutate.mockReset();
    mocks.invalidateQueries.mockReset();
    mocks.showPlatformActionSheet.mockReset();
    mocks.projectRealtime.mockReset();
    mocks.queryState.detail = {
      data: undefined,
      isLoading: false,
      error: null,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    };
    mocks.queryState.pins = {
      data: [],
      isLoading: false,
      error: null,
      refetch: vi.fn(() => Promise.resolve()),
      isRefetching: false,
    };
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("renders loading and error states", () => {
    mocks.queryState.detail.isLoading = true;

    act(() => {
      renderer = create(<ProjectDetail />);
    });

    expect(renderer!.root.findAllByType("ActivityIndicator")).toHaveLength(1);

    act(() => {
      renderer?.unmount();
      mocks.queryState.detail.isLoading = false;
      mocks.queryState.detail.error = new Error("boom");
      renderer = create(<ProjectDetail />);
    });

    const textValues = renderer!.root
      .findAllByType("Text")
      .flatMap((node) =>
        Array.isArray(node.props.children)
          ? node.props.children
          : [node.props.children],
      )
      .filter((value): value is string => typeof value === "string");
    expect(textValues.some((value) => value.includes("Failed to load project"))).toBe(true);
    act(() => {
      renderer!.root.findByType("Button").props.onPress();
    });
    expect(mocks.queryState.detail.refetch).toHaveBeenCalledTimes(1);
  });

  it("renders the success state and refreshes related data", async () => {
    mocks.queryState.detail.data = {
      id: "project-1",
      title: "Project Alpha",
      priority: "high",
    };

    await act(async () => {
      renderer = create(<ProjectDetail />);
    });

    expect(mocks.projectRealtime).toHaveBeenCalledWith(
      "project-1",
      expect.any(Function),
    );
    expect(mocks.headerCards).toHaveLength(1);
    expect(mocks.propertiesProps).toHaveLength(1);
    expect(mocks.resourcesProps).toHaveLength(1);
    expect(mocks.relatedIssueProps).toEqual([{ projectId: "project-1" }]);

    const scrollView = renderer!.root.findByType("ScrollView");
    await act(async () => {
      scrollView.props.refreshControl.props.onRefresh();
    });

    expect(mocks.queryState.detail.refetch).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateQueries).toHaveBeenCalledTimes(2);

    act(() => {
      (mocks.headerCards[0] as { onEdit: () => void }).onEdit();
      (mocks.propertiesProps[0] as {
        onPressStatus: () => void;
        onPressPriority: () => void;
        onPressLead: () => void;
      }).onPressStatus();
      (mocks.propertiesProps[0] as {
        onPressStatus: () => void;
        onPressPriority: () => void;
        onPressLead: () => void;
      }).onPressPriority();
      (mocks.propertiesProps[0] as {
        onPressStatus: () => void;
        onPressPriority: () => void;
        onPressLead: () => void;
      }).onPressLead();
      (mocks.resourcesProps[0] as { onAdd: () => void }).onAdd();
    });

    expect(mocks.routerPush).toHaveBeenNthCalledWith(1, "/alpha/project/project-1/edit");
    expect(mocks.routerPush).toHaveBeenNthCalledWith(2, {
      pathname: "/[workspace]/project/[id]/picker/status",
      params: { workspace: "alpha", id: "project-1" },
    });
    expect(mocks.routerPush).toHaveBeenNthCalledWith(3, {
      pathname: "/[workspace]/project/[id]/picker/priority",
      params: { workspace: "alpha", id: "project-1" },
    });
    expect(mocks.routerPush).toHaveBeenNthCalledWith(4, {
      pathname: "/[workspace]/project/[id]/picker/lead",
      params: { workspace: "alpha", id: "project-1" },
    });
    expect(mocks.routerPush).toHaveBeenNthCalledWith(5, {
      pathname: "/[workspace]/project/[id]/add-resource",
      params: { workspace: "alpha", id: "project-1" },
    });
  });

  it("shows project actions and executes pin/link/delete handlers", () => {
    mocks.queryState.detail.data = {
      id: "project-1",
      title: "Project Alpha",
    };
    process.env.EXPO_PUBLIC_WEB_URL = "https://app.multica.ai";

    act(() => {
      renderer = create(<ProjectDetail />);
    });

    const headerRight = mocks.stackScreens[0]?.options.headerRight as
      | (() => React.ReactElement)
      | undefined;
    act(() => {
      create(headerRight!());
    });

    act(() => {
      (mocks.iconButtons[0] as { onPress: () => void }).onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Pin",
      "Edit details",
      "Open on web",
      "Delete",
      "Cancel",
    ]);

    act(() => {
      config.options[0].onPress();
      config.options[1].onPress();
      config.options[2].onPress();
      config.options[3].onPress();
    });

    expect(mocks.createPinMutate).toHaveBeenCalledWith({
      item_type: "project",
      item_id: "project-1",
    });
    expect(mocks.routerPush).toHaveBeenCalledWith("/alpha/project/project-1/edit");
    expect(mocks.openUrl).toHaveBeenCalledWith(
      "https://app.multica.ai/alpha/projects/project-1",
    );
    expect(mocks.alertMock).toHaveBeenCalledTimes(1);

    const deleteButtons = mocks.alertMock.mock.calls[0]?.[2];
    act(() => {
      deleteButtons[1].onPress();
    });

    const deleteCall = mocks.deleteMutate.mock.calls[0];
    expect(deleteCall?.[0]).toBeUndefined();
    expect(deleteCall?.[1]).toEqual(
      expect.objectContaining({
        onSuccess: expect.any(Function),
      }),
    );
    act(() => {
      deleteCall?.[1]?.onSuccess();
    });
    expect(mocks.routerBack).toHaveBeenCalledTimes(1);
  });

  it("handles pinned projects and the empty-project fallback", () => {
    let menuRenderer: ReactTestRenderer | null = null;
    mocks.queryState.detail.data = {
      id: "project-2",
      title: "Project Beta",
    };
    mocks.queryState.pins.data = [
      { item_type: "project", item_id: "project-2" },
    ];
    delete process.env.EXPO_PUBLIC_WEB_URL;

    act(() => {
      renderer = create(<ProjectDetail />);
    });

    const headerRight = mocks.stackScreens[0]?.options.headerRight as
      | (() => React.ReactElement)
      | undefined;
    act(() => {
      mocks.iconButtons.length = 0;
      menuRenderer = create(headerRight!());
    });
    act(() => {
      (mocks.iconButtons.at(-1) as { onPress: () => void }).onPress();
    });

    const config = mocks.showPlatformActionSheet.mock.calls[0]?.[0];
    expect(config.options.map((option: { label: string }) => option.label)).toEqual([
      "Unpin",
      "Edit details",
      "Delete",
      "Cancel",
    ]);
    act(() => {
      config.options[0].onPress();
    });
    expect(mocks.deletePinMutate).toHaveBeenCalledWith({
      itemType: "project",
      itemId: "project-2",
    });

    act(() => {
      renderer?.unmount();
      mocks.queryState.detail.data = { id: "", title: "" };
      renderer = create(<ProjectDetail />);
    });
    const textValues = renderer!.root
      .findAllByType("Text")
      .flatMap((node) =>
        Array.isArray(node.props.children)
          ? node.props.children
          : [node.props.children],
      )
      .filter((value): value is string => typeof value === "string");
    expect(textValues).toContain("not found");
    (menuRenderer as ReactTestRenderer | null)?.unmount();
  });
});
