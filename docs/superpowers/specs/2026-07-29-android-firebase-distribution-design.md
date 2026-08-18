# Android Firebase Distribution Design

## Goal

Produce a signed Multica staging APK on demand in GitHub Actions and distribute it to approved testers through Firebase App Distribution.

## Scope

This first release slice includes:

- Manual GitHub Actions dispatch only.
- Android staging package `ai.multica.mobile.staging`.
- Durable release signing supplied through GitHub environment secrets.
- Firebase App Distribution upload to a configured tester group.
- Monotonic Android `versionCode` derived from the workflow run number.
- Release notes supplied by the workflow operator.
- A short-lived GitHub artifact for build diagnosis.

This slice excludes:

- Automatic distribution on pushes or merges.
- Production package `ai.multica.mobile`.
- Google Play Console submission.
- EAS Build, EAS Submit, and EAS Update.
- Firebase runtime SDKs, Crashlytics, Analytics, and Test Lab.
- Firebase project, tester, IAM, or GitHub secret creation from repository code.

## Chosen Approach

Use GitHub Actions, the existing Expo/Gradle Android project, and Firebase CLI.

Alternatives considered:

1. **EAS Build plus Firebase App Distribution.** EAS can manage signing, but adds another build service, token, artifact handoff, and billing surface before Android needs it.
2. **Firebase App Distribution Gradle plugin.** Upload stays inside Gradle, but Expo prebuild would require more generated-native configuration and couple ordinary Android builds to Firebase distribution.
3. **Local developer distribution.** Smallest code change, but provides no repeatable team release path or controlled secret boundary.

GitHub Actions plus Firebase CLI reuses current CI, keeps Firebase outside the app runtime, and leaves future Play publishing independent.

## Repository Changes

### Release signing config plugin

Create `apps/mobile/plugins/with-android-release-signing.ts`.

During `expo prebuild`, the plugin:

1. Writes `android/app/multica-release-signing.gradle`.
2. Adds one idempotent `apply from: "./multica-release-signing.gradle"` line to `android/app/build.gradle`.
3. Leaves debug builds unchanged.

The generated Gradle script reads these environment variables at build time:

- `MULTICA_ANDROID_KEYSTORE_PATH`
- `MULTICA_ANDROID_KEYSTORE_PASSWORD`
- `MULTICA_ANDROID_KEY_ALIAS`
- `MULTICA_ANDROID_KEY_PASSWORD`
- `MULTICA_ANDROID_RELEASE_SIGNING_REQUIRED`

When `MULTICA_ANDROID_RELEASE_SIGNING_REQUIRED=true`, every signing value must be present and the keystore file must exist. Missing input fails the Gradle configuration before compilation. When the flag is absent, existing local debug behavior remains available.

Register the local plugin in `apps/mobile/app.config.ts`. Generated Android files remain reproducible after every variant prebuild.

### Version config

Extend `apps/mobile/app.config.ts` so `ANDROID_VERSION_CODE` controls `android.versionCode`.

- Missing value defaults to `1` for local development.
- Supplied value must be a positive integer.
- Invalid values fail Expo config evaluation with a clear message.

The workflow sets `ANDROID_VERSION_CODE` to `github.run_number`.

### Distribution workflow

Create `.github/workflows/mobile-android-distribute.yml`.

Workflow characteristics:

- Trigger: `workflow_dispatch`.
- Input: optional release notes.
- Environment: `android-staging`.
- Permissions: `contents: read`, `id-token: write`.
- Concurrency: one staging distribution at a time, without canceling an active upload.

Workflow steps:

1. Check out the requested commit.
2. Set up pnpm, Node 22, and Java 17.
3. Install dependencies with the repository lockfile.
4. Validate required variables and secrets without printing values.
5. Decode the keystore into `$RUNNER_TEMP`.
6. Run the existing staging Expo prebuild.
7. Build `apps/mobile/android/app/build/outputs/apk/release/app-release.apk`.
8. Upload the APK as a GitHub artifact with seven-day retention.
9. Authenticate to Google Cloud through GitHub OIDC and Workload Identity Federation.
10. Run a pinned Firebase CLI version to distribute the APK with release notes and the configured tester group.

The APK artifact upload occurs before Firebase authentication so a Firebase-only failure does not discard a valid build.

## External Configuration

GitHub environment `android-staging` must contain:

### Variables

- `GCP_PROJECT_ID`
- `GCP_WORKLOAD_IDENTITY_PROVIDER`
- `GCP_SERVICE_ACCOUNT`
- `FIREBASE_APP_ID_ANDROID_STAGING`
- `FIREBASE_TESTER_GROUPS`

### Secrets

- `ANDROID_KEYSTORE_BASE64`
- `ANDROID_KEYSTORE_PASSWORD`
- `ANDROID_KEY_ALIAS`
- `ANDROID_KEY_PASSWORD`

Google Cloud must contain a dedicated service account for this workflow with Firebase App Distribution Admin access. Its Workload Identity Federation trust must be limited to this repository and the `android-staging` GitHub environment.

Firebase must contain an Android app registered with package `ai.multica.mobile.staging` and the configured tester group.

If no durable signing keystore exists, create one once outside the repository, store the original plus passwords in secure long-term storage, and place only its base64 representation and credentials in GitHub environment secrets.

## Data and Secret Flow

1. GitHub exchanges its short-lived OIDC token for Google credentials; no service-account JSON key is stored.
2. Signing secrets exist only in the environment-scoped job and decoded file under the ephemeral runner temporary directory.
3. Expo prebuild bakes the committed staging API URL and package identity into generated Android sources.
4. Gradle signs the release APK with the durable staging key.
5. Firebase CLI uploads the signed APK and grants access to the configured tester group.

No secret uses an `EXPO_PUBLIC_*` variable. No secret or generated credential file is committed or uploaded as an artifact.

## Failure Behavior

- Missing GitHub variable or secret: fail before dependency-heavy build work and list only missing names.
- Invalid `ANDROID_VERSION_CODE`: fail Expo config evaluation.
- Missing decoded keystore or signing value: fail Gradle configuration.
- Build failure: no Firebase authentication or distribution attempt.
- Firebase authentication or upload failure: retain the GitHub APK artifact for diagnosis.
- Concurrent dispatch: queue behind the active staging distribution.

## Testing

Automated tests will verify:

- Staging Expo config keeps package `ai.multica.mobile.staging`.
- `ANDROID_VERSION_CODE` defaults to `1`, accepts positive integers, and rejects invalid values.
- Signing plugin output is idempotent.
- Generated signing script reads required environment variables and fails closed when signing is required.
- Workflow remains manual-only, uses the `android-staging` environment, builds a release APK, authenticates with OIDC, and distributes through Firebase App Distribution.

Verification commands:

```bash
pnpm -C apps/mobile test
pnpm -C apps/mobile typecheck
pnpm -C apps/mobile lint
pnpm -C apps/mobile android:sync:staging
```

When a usable keystore and Android SDK are available, additionally run a staging `assembleRelease` build with signing required. Cloud distribution itself remains unexecuted until the user supplies external configuration and explicitly requests a deployment.

## Follow-up Slices

After one tester build succeeds:

1. Add Firebase Crashlytics if release telemetry is wanted.
2. Add Firebase Test Lab or App Testing agent coverage for a small device matrix.
3. Add production AAB signing and Google Play internal testing.
4. Add tag-triggered distribution only after manual releases prove stable.
