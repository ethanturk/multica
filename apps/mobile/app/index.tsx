import { ActivityIndicator, View } from "react-native";
import { Redirect } from "expo-router";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { getEntryRoute } from "@/lib/entry-route";

/**
 * Entry redirect. AuthInitializer (in _layout.tsx) finishes auth + slug
 * hydration before this renders meaningfully — until then, isLoading is true.
 *
 *   no user            → /login
 *   user, no slug      → /select-workspace
 *   user, slug         → /[slug]/inbox
 */
export default function Index() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const route = getEntryRoute({
    isLoading,
    user,
    workspaceSlug: slug,
  });

  if (!route) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  return <Redirect href={route} />;
}
