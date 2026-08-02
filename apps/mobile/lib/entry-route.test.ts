import { describe, expect, it } from "vitest";
import { getEntryRoute } from "./entry-route";

describe("getEntryRoute", () => {
  it.each([
    {
      label: "signed out state",
      user: null,
      workspaceSlug: null,
    },
    {
      label: "stale signed-in state",
      user: { id: "user-1" },
      workspaceSlug: "alpha",
    },
  ])(
    "returns null while auth initialization is still loading for $label",
    ({ user, workspaceSlug }) => {
      expect(
        getEntryRoute({
          isLoading: true,
          user,
          workspaceSlug,
        }),
      ).toBeNull();
    },
  );

  it("returns null while auth initialization is still loading", () => {
    expect(
      getEntryRoute({
        isLoading: true,
        user: null,
        workspaceSlug: null,
      }),
    ).toBeNull();
  });

  it("routes unauthenticated users to login once loading finishes", () => {
    expect(
      getEntryRoute({
        isLoading: false,
        user: null,
        workspaceSlug: null,
      }),
    ).toBe("/login");
  });

  it("routes authenticated users without a workspace to selection", () => {
    expect(
      getEntryRoute({
        isLoading: false,
        user: { id: "user-1" },
        workspaceSlug: null,
      }),
    ).toBe("/select-workspace");
  });

  it("routes authenticated users with a workspace to that inbox", () => {
    expect(
      getEntryRoute({
        isLoading: false,
        user: { id: "user-1" },
        workspaceSlug: "alpha",
      }),
    ).toBe("/alpha/inbox");
  });

  it("treats an empty slug like no workspace selection", () => {
    expect(
      getEntryRoute({
        isLoading: false,
        user: { id: "user-1" },
        workspaceSlug: "",
      }),
    ).toBe("/select-workspace");
  });

  it("ignores stale workspace state when the user is signed out", () => {
    expect(
      getEntryRoute({
        isLoading: false,
        user: null,
        workspaceSlug: "alpha",
      }),
    ).toBe("/login");
  });

  it.each([
    {
      workspaceSlug: "alpha",
      expectedRoute: "/alpha/inbox",
    },
    {
      workspaceSlug: "team-42",
      expectedRoute: "/team-42/inbox",
    },
    {
      workspaceSlug: "workspace.with.dots",
      expectedRoute: "/workspace.with.dots/inbox",
    },
    {
      workspaceSlug: "munchen",
      expectedRoute: "/munchen/inbox",
    },
  ])(
    "preserves the selected workspace slug in the inbox route for $workspaceSlug",
    ({ workspaceSlug, expectedRoute }) => {
      expect(
        getEntryRoute({
          isLoading: false,
          user: { id: "user-1" },
          workspaceSlug,
        }),
      ).toBe(expectedRoute);
    },
  );
});
