import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const parseWithFallback = vi.fn((raw) => raw);
const getCurrentSlug = vi.fn(() => "workspace-a");
const createRequestId = vi.fn(() => "rid-123");

vi.mock("@/lib/parse-response", () => ({
  parseWithFallback,
}));

vi.mock("./workspace-store", () => ({
  getCurrentSlug,
}));

vi.mock("@/lib/request-id", () => ({
  createRequestId,
}));

describe("api", () => {
  const originalApiUrl = process.env.EXPO_PUBLIC_API_URL;
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = "https://api.multica.test";
    vi.resetModules();
    vi.clearAllMocks();
    globalThis.fetch = vi.fn();
  });

  afterEach(() => {
    if (originalApiUrl === undefined) {
      delete process.env.EXPO_PUBLIC_API_URL;
    } else {
      process.env.EXPO_PUBLIC_API_URL = originalApiUrl;
    }
    globalThis.fetch = originalFetch;
  });

  it("covers the request wrappers and validation helpers across the mobile API surface", async () => {
    const schemas = await import("./schemas");
    const { api } = await import("./api");

    api.setToken("secret-token");
    api.setOptions({ onUnauthorized: vi.fn() });

    const user = { id: "user-1" };
    const workspace = { id: "ws-1" };
    const issue = { id: "issue-1" };
    const comment = { id: "comment-1" };
    const labelResponse = { labels: [] };
    const project = { id: "project-1" };
    const chatSession = { id: "chat-1", workspace_id: "ws-1", agent_id: "agent-1", title: "Hello", created_at: "2026-06-15T00:00:00Z", updated_at: "2026-06-15T00:00:00Z" };
    const sendChatResponse = {
      message_id: "msg-1",
      task_id: "task-1",
      created_at: "2026-06-15T00:00:00Z",
    };
    const attachment = {
      id: "att-1",
      issue_id: "issue-1",
      comment_id: null,
      chat_message_id: null,
      filename: "report.pdf",
      original_filename: "report.pdf",
      content_type: "application/pdf",
      size_bytes: 1,
      storage_key: "files/report.pdf",
      url: "mc://file/att-1",
      download_url: "/api/attachments/att-1/download",
      created_at: "2026-06-15T00:00:00Z",
      updated_at: "2026-06-15T00:00:00Z",
    };

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>;
    const responses = new Map<string, unknown>([
      ["POST /auth/send-code", undefined],
      ["POST /auth/verify-code", { token: "token", user }],
      ["GET /api/me", user],
      ["PATCH /api/me", user],
      ["GET /api/notification-preferences", { preferences: [] }],
      ["PUT /api/notification-preferences", { preferences: [] }],
      ["GET /api/workspaces", [workspace]],
      ["GET /api/inbox", [{ id: "inbox-1" }]],
      ["POST /api/inbox/inbox-1/read", { id: "inbox-2" }],
      ["POST /api/inbox/inbox-1/archive", { id: "inbox-3" }],
      ["POST /api/inbox/mark-all-read", { count: 1 }],
      ["POST /api/inbox/archive-all", { count: 2 }],
      ["POST /api/inbox/archive-all-read", { count: 3 }],
      ["POST /api/inbox/archive-completed", { count: 4 }],
      ["GET /api/workspaces/ws-1/members", [{ id: "member-1" }]],
      ["GET /api/agents", [{ id: "agent-1" }]],
      ["GET /api/runtimes", [{ id: "runtime-1" }]],
      ["GET /api/agent-task-snapshot", [{ id: "task-1" }]],
      ["GET /api/squads", [{ id: "squad-1" }]],
      ["GET /api/issues?status=todo%2Cdone&limit=20&offset=10", { issues: [], total: 0, next_offset: null }],
      ["GET /api/issues/search?q=hello&include_closed=true&offset=2", { issues: [], total: 0, next_offset: null }],
      ["GET /api/issues/issue-1", issue],
      ["POST /api/issues", issue],
      ["GET /api/issues/issue-1/timeline", [{ id: "timeline-1" }]],
      ["GET /api/issues/issue-1/attachments", [attachment]],
      ["GET /api/issues/issue-1/active-task", { tasks: [{ id: "active-1" }] }],
      ["GET /api/issues/issue-1/task-runs", [{ id: "run-1" }]],
      ["POST /api/issues/issue-1/comments", comment],
      ["PUT /api/comments/comment-1", comment],
      ["DELETE /api/comments/comment-1", undefined],
      ["POST /api/comments/comment-1/resolve", comment],
      ["DELETE /api/comments/comment-1/resolve", comment],
      ["POST /api/comments/comment-1/reactions", { emoji: ":+1:" }],
      ["DELETE /api/comments/comment-1/reactions", undefined],
      ["POST /api/issues/issue-1/reactions", { emoji: ":+1:" }],
      ["DELETE /api/issues/issue-1/reactions", undefined],
      ["PUT /api/issues/issue-1", issue],
      ["DELETE /api/issues/issue-1", undefined],
      ["GET /api/labels", { labels: [] }],
      ["POST /api/labels", { id: "label-1" }],
      ["POST /api/issues/issue-1/labels", labelResponse],
      ["DELETE /api/issues/issue-1/labels/label-1", labelResponse],
      ["GET /api/projects", { projects: [] }],
      ["GET /api/projects/search?q=proj&limit=1", { projects: [] }],
      ["GET /api/projects/project-1", project],
      ["POST /api/projects", project],
      ["PUT /api/projects/project-1", project],
      ["DELETE /api/projects/project-1", undefined],
      ["GET /api/projects/project-1/resources", { resources: [] }],
      ["POST /api/projects/project-1/resources", { id: "resource-1" }],
      ["DELETE /api/projects/project-1/resources/resource-1", undefined],
      ["GET /api/chat/sessions", [chatSession]],
      ["POST /api/chat/sessions", chatSession],
      ["DELETE /api/chat/sessions/chat-1", undefined],
      ["GET /api/chat/sessions/chat-1/messages", [{ id: "chat-msg-1" }]],
      ["POST /api/chat/sessions/chat-1/messages", sendChatResponse],
      ["GET /api/chat/sessions/chat-1/pending-task", { id: "pending-1" }],
      ["POST /api/chat/sessions/chat-1/read", undefined],
      ["POST /api/tasks/task-1/cancel", undefined],
      ["GET /api/tasks/task-1/messages", [{ id: "task-msg-1" }]],
      ["GET /api/pins", [{ id: "pin-1" }]],
      ["POST /api/pins", { id: "pin-1", workspace_id: "ws-1", user_id: "user-1", item_type: "issue", item_id: "issue-1", position: 1, created_at: "2026-06-15T00:00:00Z" }],
      ["DELETE /api/pins/issue/issue-1", undefined],
      ["PUT /api/pins/reorder", undefined],
      ["POST /api/upload-file", attachment],
    ]);
    fetchMock.mockImplementation(async (input, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const path = String(input).replace("https://api.multica.test", "");
      const key = `${method} ${path}`;
      const next = responses.get(key);
      return {
        ok: responses.has(key),
        status: next === undefined ? 204 : 200,
        statusText: responses.has(key) ? "OK" : "Missing",
        json: vi.fn(async () => next),
        headers: new Headers(),
      } as unknown as Response;
    });

    const sessionParseSpy = vi.spyOn(schemas.ChatSessionSchema, "safeParse");
    const sendParseSpy = vi.spyOn(schemas.SendChatMessageResponseSchema, "safeParse");

    await api.sendCode("person@example.com");
    await api.verifyCode("person@example.com", "123456");
    await api.getMe();
    await api.updateMe({ name: "Person" });
    await api.getNotificationPreferences();
    await api.updateNotificationPreferences({});
    await api.listWorkspaces();
    await api.listInbox();
    await api.markInboxRead("inbox-1");
    await api.archiveInbox("inbox-1");
    await api.markAllInboxRead();
    await api.archiveAllInbox();
    await api.archiveAllReadInbox();
    await api.archiveCompletedInbox();
    await api.listMembers("ws-1");
    await api.listAgents();
    await api.listRuntimes();
    await api.listAgentTaskSnapshot();
    await api.listSquads();
    await api.listIssues({ status: ["todo", "done"] as never, limit: 20, offset: 10 } as never);
    await api.searchIssues({ q: "hello", include_closed: true, offset: 2 });
    await api.getIssue("issue-1");
    await api.createIssue({ title: "Issue" });
    await api.listTimeline("issue-1");
    await api.listAttachments("issue-1");
    expect(await api.listActiveTasksForIssue("issue-1")).toEqual([{ id: "active-1" }]);
    await api.listTasksByIssue("issue-1");
    await api.createComment("issue-1", "Hello", { parentId: "root-1", type: "reply", attachmentIds: ["att-1"] });
    await api.updateComment("comment-1", "Updated", ["att-1"]);
    await api.deleteComment("comment-1");
    await api.resolveComment("comment-1");
    await api.unresolveComment("comment-1");
    await api.addReaction("comment-1", ":+1:");
    await api.removeReaction("comment-1", ":+1:");
    await api.addIssueReaction("issue-1", ":+1:");
    await api.removeIssueReaction("issue-1", ":+1:");
    await api.updateIssue("issue-1", { title: "Updated" });
    await api.deleteIssue("issue-1");
    await api.listLabels();
    await api.createLabel({ name: "bug", color: "#fff" });
    await api.attachLabel("issue-1", "label-1");
    await api.detachLabel("issue-1", "label-1");
    await api.listProjects();
    await api.searchProjects({ q: "proj", limit: 1 });
    await api.getProject("project-1");
    await api.createProject({ title: "Project" } as never);
    await api.updateProject("project-1", { title: "Project" } as never);
    await api.deleteProject("project-1");
    await api.listProjectResources("project-1");
    await api.createProjectResource("project-1", { resource_type: "github_repo", resource_ref: { url: "https://github.com/ethanturk/multica" } } as never);
    await api.deleteProjectResource("project-1", "resource-1");
    await api.listChatSessions();
    await api.createChatSession({ agent_id: "agent-1", title: "Hello" });
    await api.deleteChatSession("chat-1");
    await api.listChatMessages("chat-1");
    await api.sendChatMessage("chat-1", "Hi", { attachmentIds: ["att-1"] });
    await api.getPendingChatTask("chat-1");
    await api.markChatSessionRead("chat-1");
    await api.cancelTaskById("task-1");
    await api.listTaskMessages("task-1");
    await api.listPins();
    await api.createPin({ item_type: "issue", item_id: "issue-1" });
    await api.deletePin("issue", "issue-1");
    await api.reorderPins({ pin_ids: ["pin-1"] } as never);

    await api.uploadFile({
      uri: "file:///tmp/report.pdf",
      name: "report.pdf",
      type: "application/pdf",
    });

    expect(fetchMock).toHaveBeenCalledTimes(65);
    expect(parseWithFallback).toHaveBeenCalled();
    expect(sessionParseSpy).toHaveBeenCalledOnce();
    expect(sendParseSpy).toHaveBeenCalledOnce();

    const meHeaders = new Headers(fetchMock.mock.calls[2]?.[1]?.headers as HeadersInit);
    expect(meHeaders.get("Authorization")).toBe("Bearer secret-token");
    expect(meHeaders.get("X-Workspace-Slug")).toBe("workspace-a");
    expect(meHeaders.get("X-Client-OS")).toBe("ios");
    expect(createRequestId).toHaveBeenCalled();
  });

  it("covers error, timeout, strict-parse, and upload branches", async () => {
    const unauthorized = vi.fn();
    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>;

    fetchMock
      .mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: "Unauthorized",
        json: vi.fn(async () => ({ message: "bad token" })),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: "Server Error",
        json: vi.fn(async () => {
          throw new Error("not json");
        }),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: true,
        status: 204,
        statusText: "No Content",
        json: vi.fn(),
      } as unknown as Response)
      .mockRejectedValueOnce(Object.assign(new Error("aborted"), { name: "AbortError" }))
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        statusText: "OK",
        json: vi.fn(async () => ({ bad: true })),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        statusText: "OK",
        json: vi.fn(async () => ({ bad: true })),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: "Unauthorized",
        json: vi.fn(async () => {
          throw new Error("not json");
        }),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        statusText: "OK",
        json: vi.fn(async () => ({ missing: "url" })),
      } as unknown as Response);

    const { api } = await import("./api");
    const schemas = await import("./schemas");
    const attachmentParseSpy = vi.spyOn(schemas.AttachmentSchema, "safeParse");
    api.setOptions({ onUnauthorized: unauthorized });

    await expect(api.markInboxRead("inbox-401")).rejects.toMatchObject({
      message: "bad token",
      status: 401,
    });
    expect(unauthorized).toHaveBeenCalledTimes(1);

    await expect(api.archiveInbox("inbox-500")).rejects.toMatchObject({
      message: "500 Server Error",
      status: 500,
    });

    await expect(api.deleteIssue("issue-204")).resolves.toBeUndefined();

    await expect(api.listWorkspaces()).rejects.toMatchObject({
      message: "Request timed out after 30000ms",
      status: 0,
    });

    const callerAbortController = new AbortController();
    callerAbortController.abort(new Error("caller-aborted"));
    const fetchError = new Error("network boom");
    fetchMock.mockReset();
    fetchMock
      .mockImplementationOnce(async () => {
        throw callerAbortController.signal.reason;
      })
      .mockImplementationOnce(async () => {
        throw fetchError;
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        statusText: "OK",
        json: vi.fn(async () => ({ bad: true })),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        statusText: "OK",
        json: vi.fn(async () => ({ bad: true })),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: "Unauthorized",
        json: vi.fn(async () => {
          throw new Error("not json");
        }),
      } as unknown as Response)
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        statusText: "OK",
        json: vi.fn(async () => ({ missing: "url" })),
      } as unknown as Response);
    await expect(
      api.getMe({ signal: callerAbortController.signal }),
    ).rejects.toBe(callerAbortController.signal.reason);

    await expect(
      api.getMe({ signal: new AbortController().signal }),
    ).rejects.toBe(fetchError);

    await expect(api.createChatSession({ agent_id: "agent-1" })).rejects.toMatchObject({
      message: "Create chat session response invalid",
      status: 0,
    });

    await expect(api.sendChatMessage("chat-1", "Hello")).rejects.toMatchObject({
      message: "Send message response invalid",
      status: 0,
    });

    await expect(
      api.uploadFile(
        {
          uri: "file:///tmp/report.pdf",
          name: "report.pdf",
          type: "application/pdf",
        },
        { issueId: "issue-1", commentId: "comment-1" },
      ),
    ).rejects.toMatchObject({
      message: "Upload failed: 401",
      status: 401,
    });
    expect(unauthorized).toHaveBeenCalledTimes(2);

    await expect(
      api.uploadFile({
        uri: "file:///tmp/report.pdf",
        name: "report.pdf",
        type: "application/pdf",
      }),
    ).rejects.toMatchObject({
      message: "Upload response invalid",
      status: 200,
    });

    expect(attachmentParseSpy).toHaveBeenCalledOnce();
  });
});
