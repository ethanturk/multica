export type EntryRoute =
  | "/login"
  | "/select-workspace"
  | `/${string}/inbox`;

interface GetEntryRouteParams {
  isLoading: boolean;
  user: unknown | null;
  workspaceSlug: string | null;
}

export function getEntryRoute({
  isLoading,
  user,
  workspaceSlug,
}: GetEntryRouteParams): EntryRoute | null {
  if (isLoading) {
    return null;
  }

  if (!user) {
    return "/login";
  }

  if (!workspaceSlug) {
    return "/select-workspace";
  }

  return `/${workspaceSlug}/inbox`;
}
