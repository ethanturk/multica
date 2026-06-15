import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";

const mocks = vi.hoisted(() => ({
  stackScreens: [] as Array<Record<string, any>>,
  redirects: [] as Array<Record<string, any>>,
  queryState: {
    data: undefined as Array<{ id: string; slug: string }> | undefined,
    isLoading: false,
  },
  workspaceSlug: "alpha",
  setCurrentWorkspace: vi.fn(),
  inboxRealtime: vi.fn(),
  issuesRealtime: vi.fn(),
  myIssuesRealtime: vi.fn(),
  chatSessionsRealtime: vi.fn(),
  projectsRealtime: vi.fn(),
  pinsRealtime: vi.fn(),
  presenceRealtime: vi.fn(),
  presencePrefetch: vi.fn(),
  newIssueReset: vi.fn(),
  newProjectReset: vi.fn(),
  chatPickerReset: vi.fn(),
}));

vi.mock("react-native", () => ({
  Platform: {
    OS: "ios",
    select: (values: { ios?: string; default?: string }) => values.ios ?? values.default,
  },
}));

vi.mock("expo-router", () => ({
  Redirect: (props: Record<string, any>) => {
    mocks.redirects.push(props);
    return React.createElement("Redirect", props);
  },
  Stack: Object.assign(
    (props: Record<string, any>) => React.createElement("Stack", props, props.children),
    {
      Screen: (props: Record<string, any>) => {
        mocks.stackScreens.push(props);
        return React.createElement("StackScreen", props);
      },
    },
  ),
  useLocalSearchParams: () => ({
    workspace: mocks.workspaceSlug,
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => mocks.queryState,
}));

vi.mock("@/data/queries/workspaces", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (state: { setCurrentWorkspace: typeof mocks.setCurrentWorkspace }) => unknown,
  ) => selector({ setCurrentWorkspace: mocks.setCurrentWorkspace }),
}));

vi.mock("@/data/realtime/realtime-provider", () => ({
  RealtimeProvider: (props: Record<string, any>) =>
    React.createElement("RealtimeProvider", props, props.children),
}));

vi.mock("@/data/realtime/use-inbox-realtime", () => ({
  useInboxRealtime: () => mocks.inboxRealtime(),
}));

vi.mock("@/data/realtime/use-issues-realtime", () => ({
  useIssuesRealtime: () => mocks.issuesRealtime(),
}));

vi.mock("@/data/realtime/use-my-issues-realtime", () => ({
  useMyIssuesRealtime: () => mocks.myIssuesRealtime(),
}));

vi.mock("@/data/realtime/use-chat-sessions-realtime", () => ({
  useChatSessionsRealtime: () => mocks.chatSessionsRealtime(),
}));

vi.mock("@/data/realtime/use-projects-realtime", () => ({
  useProjectsRealtime: () => mocks.projectsRealtime(),
}));

vi.mock("@/data/realtime/use-pins-realtime", () => ({
  usePinsRealtime: () => mocks.pinsRealtime(),
}));

vi.mock("@/data/realtime/use-presence-realtime", () => ({
  usePresenceRealtime: () => mocks.presenceRealtime(),
}));

vi.mock("@/lib/use-workspace-presence-prefetch", () => ({
  useWorkspacePresencePrefetch: () => mocks.presencePrefetch(),
}));

vi.mock("@/components/ui/modal-close-button", () => ({
  ModalCloseButton: () => React.createElement("ModalCloseButton"),
}));

vi.mock("@/data/stores/new-issue-draft-store", () => ({
  useNewIssueDraftResetOnWorkspaceChange: (id: string | null) => mocks.newIssueReset(id),
}));

vi.mock("@/data/stores/new-project-draft-store", () => ({
  useNewProjectDraftResetOnWorkspaceChange: (id: string | null) => mocks.newProjectReset(id),
}));

vi.mock("@/data/stores/chat-session-picker-store", () => ({
  useChatSessionPickerResetOnWorkspaceChange: (id: string | null) => mocks.chatPickerReset(id),
}));

import WorkspaceLayout, { unstable_settings } from "./_layout";

describe("WorkspaceLayout", () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
    mocks.stackScreens.length = 0;
    mocks.redirects.length = 0;
    mocks.queryState.data = undefined;
    mocks.queryState.isLoading = false;
    mocks.workspaceSlug = "alpha";
    mocks.setCurrentWorkspace.mockReset();
    mocks.inboxRealtime.mockReset();
    mocks.issuesRealtime.mockReset();
    mocks.myIssuesRealtime.mockReset();
    mocks.chatSessionsRealtime.mockReset();
    mocks.projectsRealtime.mockReset();
    mocks.pinsRealtime.mockReset();
    mocks.presenceRealtime.mockReset();
    mocks.presencePrefetch.mockReset();
    mocks.newIssueReset.mockReset();
    mocks.newProjectReset.mockReset();
    mocks.chatPickerReset.mockReset();
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  it("exports the expected deep-link anchor", () => {
    expect(unstable_settings).toEqual({ anchor: "(tabs)" });
  });

  it("returns null while the workspace list is loading", () => {
    mocks.queryState.isLoading = true;

    act(() => {
      renderer = create(<WorkspaceLayout />);
    });

    expect(renderer!.toJSON()).toBeNull();
  });

  it("redirects when the workspace slug is not a member workspace", () => {
    mocks.queryState.data = [{ id: "ws-2", slug: "beta" }];

    act(() => {
      renderer = create(<WorkspaceLayout />);
    });

    expect(mocks.redirects).toEqual([{ href: "/select-workspace" }]);
    expect(mocks.newIssueReset).toHaveBeenCalledWith(null);
    expect(mocks.newProjectReset).toHaveBeenCalledWith(null);
    expect(mocks.chatPickerReset).toHaveBeenCalledWith(null);
  });

  it("sets the workspace, mounts realtime subscriptions, and configures stack screens", () => {
    mocks.queryState.data = [{ id: "ws-1", slug: "alpha" }];

    act(() => {
      renderer = create(<WorkspaceLayout />);
    });

    expect(mocks.setCurrentWorkspace).toHaveBeenCalledWith("ws-1", "alpha");
    expect(mocks.inboxRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.issuesRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.myIssuesRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.chatSessionsRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.projectsRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.pinsRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.presencePrefetch).toHaveBeenCalledTimes(1);
    expect(mocks.presenceRealtime).toHaveBeenCalledTimes(1);
    expect(mocks.newIssueReset).toHaveBeenCalledWith("ws-1");
    expect(mocks.newProjectReset).toHaveBeenCalledWith("ws-1");
    expect(mocks.chatPickerReset).toHaveBeenCalledWith("ws-1");

    const screenNames = mocks.stackScreens.map((screen) => screen.name);
    expect(screenNames).toContain("(tabs)");
    expect(screenNames).toContain("issue/[id]");
    expect(screenNames).toContain("project/[id]");
    expect(screenNames).toContain("issue/[id]/picker/assignee");
    expect(screenNames).toContain("project/[id]/add-resource");
    expect(screenNames).toContain("new-issue-picker/assignee");
    expect(screenNames).toContain("issues-filter");
    expect(screenNames).toContain("switch-workspace");
    expect(screenNames).toContain("search");

    const issueScreen = mocks.stackScreens.find((screen) => screen.name === "issue/[id]");
    expect(issueScreen).toEqual(
      expect.objectContaining({
        options: expect.objectContaining({
          title: "Issue",
          headerBackTitle: "Back",
        }),
      }),
    );

    const sheetScreenNames = [
      "issue/[id]/picker/status",
      "issue/[id]/picker/priority",
      "issue/[id]/picker/label",
      "issue/[id]/picker/project",
      "issue/[id]/picker/due-date",
      "issue/[id]/runs",
      "issue/[id]/comment/[commentId]/emoji-picker",
      "project/[id]/picker/status",
      "project/[id]/picker/priority",
      "project/[id]/picker/lead",
      "project/[id]/add-resource",
      "new-issue-picker/status",
      "new-issue-picker/priority",
      "new-issue-picker/project",
      "new-issue-picker/due-date",
      "new-project-picker/status",
      "new-project-picker/priority",
      "issues-filter",
      "chat-sessions",
      "switch-workspace",
    ];
    for (const name of sheetScreenNames) {
      const screen = mocks.stackScreens.find((item) => item.name === name);
      expect(screen).toEqual(
        expect.objectContaining({
          options: expect.objectContaining({
            presentation: "formSheet",
            headerShown: false,
          }),
        }),
      );
    }

    const titledSheetNames = ["issue/[id]/picker/assignee", "mention-picker", "new-issue-picker/assignee"];
    for (const name of titledSheetNames) {
      const screen = mocks.stackScreens.find((item) => item.name === name);
      expect(screen?.options).toEqual(
        expect.objectContaining({
          presentation: "formSheet",
          headerShown: true,
        }),
      );
    }

    const modalNames = ["project/[id]/edit", "issue/[id]/edit", "project/new", "new-issue", "search"];
    for (const name of modalNames) {
      const screen = mocks.stackScreens.find((item) => item.name === name);
      expect(screen?.options.presentation).toBe("modal");
      expect(screen?.options.headerLeft).toBeTypeOf("function");
      const headerLeft = screen?.options.headerLeft as (() => React.ReactElement) | undefined;
      expect((headerLeft?.().type as { name?: string })?.name).toBe("ModalCloseButton");
    }
  });
});
