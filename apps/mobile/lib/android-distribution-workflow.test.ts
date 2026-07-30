import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { parse } from "yaml";

type WorkflowStep = {
  name?: string;
  uses?: string;
  run?: string;
  with?: Record<string, unknown>;
};

type DistributionWorkflow = {
  on: Record<string, unknown>;
  permissions: Record<string, string>;
  concurrency: {
    group: string;
    "cancel-in-progress": boolean;
  };
  jobs: {
    distribute: {
      environment: string;
      "runs-on": string;
      env: Record<string, string>;
      steps: WorkflowStep[];
    };
  };
};

const workflowPath = decodeURIComponent(
  new URL(
    "../../../.github/workflows/mobile-android-distribute.yml",
    import.meta.url,
  ).pathname,
);

const readWorkflow = () =>
  parse(readFileSync(workflowPath, "utf8")) as DistributionWorkflow;

const findStep = (steps: WorkflowStep[], name: string) => {
  const step = steps.find((candidate) => candidate.name === name);
  expect(step, `Missing workflow step: ${name}`).toBeDefined();
  return step!;
};

describe("Android staging distribution workflow", () => {
  it("is manual-only, serialized, and uses least-privilege OIDC permissions", () => {
    const workflow = readWorkflow();

    expect(Object.keys(workflow.on)).toEqual(["workflow_dispatch"]);
    expect(workflow.permissions).toEqual({
      contents: "read",
      "id-token": "write",
    });
    expect(workflow.concurrency).toEqual({
      group: "android-staging-distribution",
      "cancel-in-progress": false,
    });
    expect(workflow.jobs.distribute.environment).toBe("android-staging");
    expect(workflow.jobs.distribute["runs-on"]).toBe("ubuntu-latest");
  });

  it("maps every external input without embedding a credential", () => {
    const { env } = readWorkflow().jobs.distribute;

    expect(env).toMatchObject({
      ANDROID_VERSION_CODE: "${{ github.run_number }}",
      MULTICA_ANDROID_RELEASE_SIGNING_REQUIRED: "true",
      GCP_PROJECT_ID: "${{ vars.GCP_PROJECT_ID }}",
      GCP_WORKLOAD_IDENTITY_PROVIDER:
        "${{ vars.GCP_WORKLOAD_IDENTITY_PROVIDER }}",
      GCP_SERVICE_ACCOUNT: "${{ vars.GCP_SERVICE_ACCOUNT }}",
      FIREBASE_APP_ID_ANDROID_STAGING:
        "${{ vars.FIREBASE_APP_ID_ANDROID_STAGING }}",
      FIREBASE_TESTER_GROUPS: "${{ vars.FIREBASE_TESTER_GROUPS }}",
      ANDROID_KEYSTORE_BASE64: "${{ secrets.ANDROID_KEYSTORE_BASE64 }}",
      MULTICA_ANDROID_KEYSTORE_PASSWORD:
        "${{ secrets.ANDROID_KEYSTORE_PASSWORD }}",
      MULTICA_ANDROID_KEY_ALIAS: "${{ secrets.ANDROID_KEY_ALIAS }}",
      MULTICA_ANDROID_KEY_PASSWORD: "${{ secrets.ANDROID_KEY_PASSWORD }}",
    });
  });

  it("validates configuration, builds staging, preserves the APK, then distributes it", () => {
    const { steps } = readWorkflow().jobs.distribute;
    const validate = findStep(steps, "Validate distribution configuration");
    const install = findStep(steps, "Install dependencies");
    const decode = findStep(steps, "Decode Android keystore");
    const prebuild = findStep(steps, "Generate staging Android project");
    const build = findStep(steps, "Build signed staging APK");
    const artifact = findStep(steps, "Upload staging APK artifact");
    const auth = findStep(steps, "Authenticate to Google Cloud");
    const distribute = findStep(steps, "Distribute APK to Firebase testers");

    expect(steps.indexOf(validate)).toBeLessThan(steps.indexOf(install));
    expect(steps.indexOf(artifact)).toBeLessThan(steps.indexOf(auth));
    expect(steps.indexOf(auth)).toBeLessThan(steps.indexOf(distribute));

    expect(validate.run).toContain("missing_names");
    expect(validate.run).not.toContain("set -x");
    expect(decode.run).toContain("$RUNNER_TEMP/multica-staging.jks");
    expect(decode.run).toContain("$GITHUB_ENV");
    expect(prebuild.run).toBe("pnpm -C apps/mobile android:sync:staging");
    expect(build.run).toContain("dotenv -e .env.staging");
    expect(build.run).toContain("APP_ENV=staging");
    expect(build.run).toContain("./gradlew assembleRelease --no-daemon");

    expect(artifact.uses).toBe("actions/upload-artifact@v4");
    expect(artifact.with).toMatchObject({
      path: "apps/mobile/android/app/build/outputs/apk/release/app-release.apk",
      "retention-days": 7,
    });
    expect(auth.uses).toBe("google-github-actions/auth@v3");
    expect(distribute.run).toContain(
      "pnpm dlx firebase-tools@15.24.0 appdistribution:distribute",
    );
    expect(distribute.run).toContain("--app \"$FIREBASE_APP_ID_ANDROID_STAGING\"");
    expect(distribute.run).toContain("--groups \"$FIREBASE_TESTER_GROUPS\"");
    expect(distribute.run).toContain("--release-notes \"$release_notes\"");
  });
});
