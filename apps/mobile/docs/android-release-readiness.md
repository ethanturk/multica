# Android release readiness

The repository can produce and distribute a signed staging APK. Public production release through Google Play is not implemented.

## Current repository path

- Local commands `pnpm android:mobile`, `pnpm android:mobile:staging`, and `pnpm android:mobile:prod` still build debug installs for an emulator or attached device.
- `.github/workflows/mobile-android-distribute.yml` is a manual-only workflow for package `ai.multica.mobile.staging`.
- `app.config.ts` derives Android `versionCode` from `ANDROID_VERSION_CODE`; the workflow supplies `github.run_number`.
- `plugins/with-android-release-signing.ts` regenerates the Gradle signing overlay during every Expo prebuild.
- When `MULTICA_ANDROID_RELEASE_SIGNING_REQUIRED=true`, Gradle fails before compilation unless all signing values exist and the keystore path is valid.
- Without that flag, local development keeps the existing debug-signing behavior.
- A built APK is retained as a GitHub artifact for seven days before Firebase authentication and upload.
- Firebase authentication uses GitHub OIDC and Google Workload Identity Federation, not a stored service-account JSON key.

## External prerequisites

Create a protected GitHub environment named `android-staging`.

Environment variables:

| Name | Purpose |
|---|---|
| `GCP_PROJECT_ID` | Google Cloud project that owns the Firebase project and workload identity configuration |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Full Workload Identity provider resource name trusted by GitHub |
| `GCP_SERVICE_ACCOUNT` | Dedicated service-account email used for Firebase App Distribution |
| `FIREBASE_APP_ID_ANDROID_STAGING` | Firebase Android App ID registered for `ai.multica.mobile.staging` |
| `FIREBASE_TESTER_GROUPS` | One or more Firebase App Distribution tester group aliases |

Environment secrets:

| Name | Purpose |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | Base64 representation of the durable staging keystore |
| `ANDROID_KEYSTORE_PASSWORD` | Staging keystore password |
| `ANDROID_KEY_ALIAS` | Alias of the signing key inside the keystore |
| `ANDROID_KEY_PASSWORD` | Password for the signing key |

Outside GitHub:

1. Register an Android app with package `ai.multica.mobile.staging` in Firebase.
2. Create the Firebase tester group aliases referenced by `FIREBASE_TESTER_GROUPS`.
3. Create a dedicated Google service account with Firebase App Distribution access.
4. Configure Workload Identity Federation to trust only this repository and the `android-staging` GitHub environment.
5. Create the staging keystore once, then store the original keystore and credentials in durable secure storage. Never rely on GitHub as the only copy.

## Run a tester distribution

1. Open GitHub Actions.
2. Select **Android Staging Distribution**.
3. Choose the commit or branch and select **Run workflow**.
4. Optionally provide tester-facing release notes.
5. After the build completes, keep the GitHub APK artifact for diagnosis or let Firebase notify the configured tester groups.

The workflow validates configuration before dependency installation. A build failure never attempts Firebase authentication. A Firebase-only failure leaves the signed APK artifact available for seven days.

## Secrets and credentials policy

- Do not commit keystores, passwords, Google credential files, Firebase tokens, or Play Console credentials.
- Do not put secrets in `EXPO_PUBLIC_*`; those values are included in the client bundle.
- Do not add `google-services.json` for App Distribution alone. The application has no Firebase runtime dependency in this release slice.
- Rotate the staging key only through an intentional migration. Testers cannot update an installed APK if a later build uses a different signing key.

## Verification before sending a build

- Run the Android smoke checklist in [`android-smoke-test.md`](./android-smoke-test.md).
- Test both a fresh install and an update install on a physical Android device.
- Confirm the staging API URL and websocket connection.
- Confirm logout and session expiry return the user to `/login`.
- Confirm the signed APK launches without Metro or a connected development machine.

## Deferred production work

- Production package `ai.multica.mobile` and durable production signing policy.
- Production AAB generation and Google Play internal testing/submission.
- Play Store listing assets, privacy policy, support contact, and release notes process.
- Firebase Crashlytics or Analytics, if approved.
- Firebase Test Lab or another automated device matrix.
- Automatic tag or merge distribution; the staging workflow remains manual until it has proven reliable.
