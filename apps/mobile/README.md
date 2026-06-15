# Multica Mobile (iOS + Android)

Expo + React Native mobile client for Multica. Independent from web/desktop — shares only types from `@multica/core/`. See [`CLAUDE.md`](./CLAUDE.md) for the locked tech-stack baseline and import rules.

## Just want to use it on your phone? (no development)

Multica isn't in the App Store or Play Store yet. Today the supported self-serve path is iPhone via Xcode:

```bash
pnpm ios:mobile:device:prod:release
```

This connects to the same backend as `multica.ai`, so your existing account just works.

**Prerequisites**: Mac with Xcode, a free Apple ID added under Xcode → Settings → Accounts, iPhone connected via USB with [Developer Mode enabled](https://docs.expo.dev/guides/ios-developer-mode/). Walk through Expo's [Set up your environment](https://docs.expo.dev/get-started/set-up-your-environment/) (pick **Development build → iOS Device**) if any of that is missing.

Xcode signs the build with the "Personal Team" your Apple ID automatically owns — created silently the first time you signed into Xcode, no setup needed. The first build downloads CocoaPods + compiles React Native from source — expect 10–20 minutes. Subsequent builds reuse Xcode's cache.

**If Xcode rejects signing with "No matching provisioning profiles found"** — rare, happens if someone has claimed the default bundle id `ai.multica.mobile` on Apple's developer portal. Pick any reverse-domain you own and re-run:

```bash
export EXPO_BUNDLE_IDENTIFIER_PROD=com.yourname.multica
pnpm ios:mobile:device:prod:release
```

**7-day signing limit**: a free Apple ID signs builds for 7 days. After that, plug back into the Mac and re-run the command to re-sign. An Apple Developer Program account ($99/yr) extends this to 1 year.

Android development support now lives in this package too, but release/distribution signing is still a follow-up task. Everything below is for app developers.

Focused Android validation and release-gap docs live here:

- [`docs/android-smoke-test.md`](./docs/android-smoke-test.md)
- [`docs/android-release-readiness.md`](./docs/android-release-readiness.md)

## Scripts

| Command | What it does | Backend |
|---|---|---|
| `pnpm dev:mobile` | Metro only (reuse existing install) | local (`.env.development.local`) |
| `pnpm dev:mobile:staging` | Metro only (reuse existing install) | staging (`.env.staging`) |
| `pnpm dev:mobile:prod` | Metro only (reuse existing install) | production (`.env.production`) |
| `pnpm android:mobile` | Full rebuild + install on **Android Emulator / attached device**, Debug | local |
| `pnpm android:mobile:staging` | Full rebuild + install on **Android Emulator / attached device**, Debug | staging |
| `pnpm android:mobile:prod` | Full rebuild + install on **Android Emulator / attached device**, Debug | production |
| `pnpm ios:mobile` | Full rebuild + install on **iOS Simulator**, Debug | local |
| `pnpm ios:mobile:staging` | Full rebuild + install on **iOS Simulator**, Debug | staging |
| `pnpm ios:mobile:prod` | Full rebuild + install on **iOS Simulator**, Debug | production |
| `pnpm ios:mobile:device` | Full rebuild + install on **USB iPhone**, Debug | local |
| `pnpm ios:mobile:device:staging` | Full rebuild + install on **USB iPhone**, Debug | staging |
| `pnpm ios:mobile:device:staging:release` | Full rebuild + install on **USB iPhone**, Release (standalone) | staging |
| `pnpm ios:mobile:device:prod` | Full rebuild + install on **USB iPhone**, Debug | production |
| `pnpm ios:mobile:device:prod:release` | Full rebuild + install on **USB iPhone**, Release (standalone) | production |

`dev:*` runs Metro only — assumes the matching variant is already installed. `android:mobile*` and `ios:mobile*` do a full native rebuild + install.

Android package name, iOS bundle id, and display name switch on `APP_ENV` (see `app.config.ts`), so Dev / Staging / Production variants can coexist on the same device or simulator.

## First-time setup

`.env.staging` is committed (public staging URL). `.env.development.local` is gitignored — copy the template once:

```bash
cp apps/mobile/.env.example apps/mobile/.env.development.local
# then edit EXPO_PUBLIC_API_URL for your target:
#   - Android Emulator: http://10.0.2.2:8080
#   - Physical iPhone / Android device: http://<your-mac-lan-ip>:8080
```

If your Apple ID isn't on the Multica Apple Developer team yet, also uncomment and set `EXPO_BUNDLE_IDENTIFIER_DEV` to a reverse-domain you own (e.g. `com.yourname.multica.dev`). This **only** overrides the dev variant — staging / production bundle ids are intentionally not overridable so variants can coexist.

## Run it on Android

### Android Emulator

```bash
pnpm android:mobile:staging
```

Requires Android Studio, an SDK platform installed, and either a booted emulator or a USB device visible to `adb devices`. Expo/Gradle pick the active target automatically.

For a local backend, set `EXPO_PUBLIC_API_URL=http://10.0.2.2:8080` in `.env.development.local`. `10.0.2.2` is the Android emulator alias back to your host machine.

### Physical Android device

Use the same `pnpm android:mobile*` command, but point `EXPO_PUBLIC_API_URL` at your Mac's LAN IP instead of `10.0.2.2`. USB debugging must be enabled on the device.

## Android release readiness

Android debug installs are ready for local development and smoke testing, but production release setup is intentionally incomplete. Before a Play Store or tester-facing release, review [`docs/android-release-readiness.md`](./docs/android-release-readiness.md) and complete the signing, store-asset, and secret-management work there.

## Build it onto your iPhone

Two paths, depending on what you want to do:

### Day-to-day development (Mac in front of you)

```bash
pnpm ios:mobile:device:staging
```

Produces a **Debug build** with `expo-dev-launcher` embedded. Every launch the app probes Metro on your Mac and pulls fresh JS — perfect for hot-reload, painful when the Mac is asleep or you're on a different WiFi.

### Standalone / "just use it" (walk away from the Mac)

```bash
pnpm ios:mobile:device:staging:release
```

Produces a **Release build**. No `expo-dev-launcher`, no Metro probe, no "Downloading…" screen. Splash → app, exactly like an App Store install. Trade-off: every JS change requires re-running this command.

Both paths share the same prerequisites: Mac with Xcode, free Apple ID added under Xcode → Settings → Accounts, iPhone connected via USB with Developer Mode enabled. Follow Expo's [Set up your environment](https://docs.expo.dev/get-started/set-up-your-environment/) — pick **Development build → iOS Device** — if any of that is missing.

First build of either variant downloads CocoaPods + compiles React Native from source — expect 10-20 minutes. Subsequent builds reuse Xcode's DerivedData cache.

## Try it in the iOS Simulator (no iPhone needed)

```bash
pnpm ios:mobile:staging
```

Boots the simulator, builds, installs the dev-client. Faster to iterate than a device build because no signing / provisioning step. Same `dev:mobile:staging` Metro flow afterward.

## 7-day signing limit (device only)

A free Apple ID signs builds for **7 days only**, Debug and Release both. After that the app refuses to launch on the iPhone. Plug back into the Mac and re-run the corresponding `ios:mobile:device*` script to re-sign. Simulator builds are unaffected. The only workaround for the device limit is an Apple Developer Program account ($99/yr), which extends to 1 year.

## Pointing at a different backend

Edit `EXPO_PUBLIC_API_URL` in `.env.staging`, `.env.production`, or `.env.development.local` (whichever variant you're running). Then:

- For an installed **Debug build**: restart Metro (`pnpm dev:mobile:staging`) so the next JS bundle picks up the new value.
- For an installed **Release build**: re-run the `ios:mobile:device:staging:release` command — the value is baked into the embedded bundle at build time.

Mobile derives `ws://` / `wss://` from that same value, and both HTTP plus realtime connections now report the actual client OS (`android` or `ios`) to the backend.

For local backend testing:

- Android Emulator: use `10.0.2.2`
- Physical iPhone / Android device: use your Mac's LAN IP (`ipconfig getifaddr en0`)
