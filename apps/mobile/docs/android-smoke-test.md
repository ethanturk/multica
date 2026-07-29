# Android smoke test

Use this checklist before handing the Android app to another developer or cutting a release candidate. The goal is narrow coverage for startup, auth/session behavior, and core navigation on real Android hardware or an emulator.

## Test setup

- Install the app with one of the documented scripts in [`../README.md`](../README.md).
- Use staging unless you are specifically validating a local backend flow.
- If you are using a local backend:
  - Android Emulator: set `EXPO_PUBLIC_API_URL=http://10.0.2.2:8080`
  - Physical Android device: set `EXPO_PUBLIC_API_URL=http://<your-mac-lan-ip>:8080`
- Start from a clean app state when validating first-launch or logout behavior:

```bash
adb uninstall ai.multica.mobile.dev
pnpm android:mobile:staging
```

## Startup and auth

1. Fresh install, signed out:
   - Launch the app.
   - Confirm the splash/loading state resolves to `/login`.
   - Enter an email address and request a login code.
   - Verify the app advances to the code verification screen.
2. Successful verification:
   - Enter a valid code.
   - Confirm the app lands on workspace selection if no workspace has been chosen yet.
   - Select a workspace and confirm the app redirects into that workspace inbox.
3. Persisted session restore:
   - Force close the app.
   - Re-open it without clearing data.
   - Confirm it restores the previous session and workspace without flashing back through `/login` or `/select-workspace`.
4. Expired or revoked session:
   - Invalidate the token from another client or backend admin flow.
   - Re-open the app or trigger an authenticated API request.
   - Confirm a backend `401` signs the user out, clears the workspace selection, and routes back to `/login`.

## Core navigation

1. Workspace inbox:
   - Confirm the initial authenticated destination is `/<workspace>/inbox`.
   - Open an item from the inbox and verify navigation into the issue/chat/detail flow succeeds.
2. Tab and stack navigation:
   - Move across the primary tabs/screens available from the selected workspace.
   - Confirm back navigation returns to the expected prior screen without losing the active workspace.
3. Workspace switching:
   - Switch to a different workspace.
   - Confirm subsequent navigation uses the new workspace slug and the inbox reflects that workspace’s data.

## Expected failures to call out immediately

- App loops on the loading spinner instead of reaching `/login`, `/select-workspace`, or `/<workspace>/inbox`
- Login succeeds but the app loses the selected workspace on every cold start
- A `401` leaves the user on an authenticated screen instead of returning to `/login`
- Android Emulator cannot reach the local backend because `localhost` was used instead of `10.0.2.2`
