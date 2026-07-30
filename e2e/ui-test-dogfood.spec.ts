import { expect, test, type Page } from "@playwright/test";

test.skip(
  !process.env.MULTICA_UI_TEST_STORAGE_STATE,
  "managed UI-test storage state is not configured",
);

async function openAuthenticatedWorkspace(page: Page): Promise<string> {
  await page.goto("/", { waitUntil: "domcontentloaded" });
  await page.waitForURL((url) => {
    const segments = url.pathname.split("/").filter(Boolean);
    return segments.length >= 2 && !["login", "onboarding"].includes(segments[0]);
  });

  const pathname = new URL(page.url()).pathname;
  expect(pathname).not.toMatch(/^\/(?:login|onboarding)(?:\/|$)/);
  const workspaceSlug = pathname.split("/").filter(Boolean)[0];
  expect(workspaceSlug).toBeTruthy();
  return workspaceSlug;
}

test("root opens an authenticated workspace", async ({ page }) => {
  await openAuthenticatedWorkspace(page);
  await expect(page).not.toHaveURL(/\/login(?:[/?#]|$)/);
});

test("Issues exposes New Issue", async ({ page }) => {
  const workspaceSlug = await openAuthenticatedWorkspace(page);
  await page.goto(`/${workspaceSlug}/issues`);
  await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible();
});

test("New Issue opens and cancels without creating data", async ({ page }) => {
  const workspaceSlug = await openAuthenticatedWorkspace(page);
  await page.goto(`/${workspaceSlug}/issues`);
  await page.getByRole("button", { name: "New Issue" }).click();

  const dialog = page.getByRole("dialog", {
    name: /^(?:New Issue|Quick create issue)$/,
  });
  await expect(dialog).toBeVisible();
  await dialog.press("Escape");
  await expect(dialog).toBeHidden();
});

test("workspace settings renders its surface", async ({ page }) => {
  const workspaceSlug = await openAuthenticatedWorkspace(page);
  await page.goto(`/${workspaceSlug}/settings`);
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" }),
  ).toBeVisible();
});
