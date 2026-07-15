# Android release readiness

This repo can build and run the Android app locally today, but it is not ready to produce a production Play Store artifact without external release inputs. This document is the checklist of what still has to happen and what must stay out of git.

## Current state in the repo

- `pnpm android:mobile`, `pnpm android:mobile:staging`, and `pnpm android:mobile:prod` build debug installs for the active emulator or attached device.
- `apps/mobile/android/app/build.gradle` currently signs the `release` build type with the debug keystore:

```gradle
release {
    signingConfig signingConfigs.debug
}
```

- No Play Console metadata, screenshots, or production signing credentials are stored in the repository.

## Required before any real release

1. Replace debug signing for `release` builds.
   - Generate or provision the production Android keystore outside the repo.
   - Inject keystore path, alias, and passwords through local secure files or CI secrets.
   - Update the Android release signing configuration to use those external inputs instead of `signingConfigs.debug`.
2. Decide the release pipeline.
   - Confirm whether releases will ship through local Gradle builds, EAS Build, or another CI path.
   - Make sure the chosen path can access the same external signing material without committing it.
3. Prepare Play Store assets.
   - Final app icon
   - Feature graphic
   - Phone/tablet screenshots
   - Store listing copy, privacy policy URL, and support contact
4. Validate production configuration.
   - Confirm the production API base URL in `apps/mobile/.env.production`
   - Verify websocket connectivity against the production backend
   - Confirm crash/reporting or analytics decisions before public rollout
5. Finalize versioning.
   - Bump `versionCode` and `versionName` in `apps/mobile/android/app/build.gradle`
   - Align any release tags or changelog process with the chosen distribution path

## Secrets and credentials policy

- Do not commit keystores, passwords, service-account JSON, or Play Console credentials.
- Do not add production-only API secrets to `apps/mobile/.env.production`; `EXPO_PUBLIC_*` values are client-visible.
- Keep secret material in developer-local secure storage or CI secret management, then inject it at build time.

## Suggested pre-release verification

- Run the Android smoke checklist in [`android-smoke-test.md`](./android-smoke-test.md).
- Test both a fresh install and an update install on a real Android device.
- Confirm logout/session-expiry behavior still returns the user to `/login`.
- Verify the signed artifact installs cleanly on a tester device without a connected dev machine.
