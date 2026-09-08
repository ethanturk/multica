import { writeFile } from "node:fs/promises";
import { join } from "node:path";

import type { ConfigPlugin } from "expo/config-plugins.js";
import configPlugins from "expo/config-plugins.js";

const { withAppBuildGradle, withDangerousMod } = configPlugins;

export const RELEASE_SIGNING_APPLY =
  'apply from: "./multica-release-signing.gradle"';

export const RELEASE_SIGNING_GRADLE = `def multicaSigningRequired =
    (System.getenv("MULTICA_ANDROID_RELEASE_SIGNING_REQUIRED") ?: "false").toBoolean()
def multicaSigningEnvironment = [
    MULTICA_ANDROID_KEYSTORE_PATH: System.getenv("MULTICA_ANDROID_KEYSTORE_PATH"),
    MULTICA_ANDROID_KEYSTORE_PASSWORD: System.getenv("MULTICA_ANDROID_KEYSTORE_PASSWORD"),
    MULTICA_ANDROID_KEY_ALIAS: System.getenv("MULTICA_ANDROID_KEY_ALIAS"),
    MULTICA_ANDROID_KEY_PASSWORD: System.getenv("MULTICA_ANDROID_KEY_PASSWORD"),
]
def multicaMissingSigningEnvironment = multicaSigningEnvironment
    .findAll { name, value -> !value }
    .keySet()
    .sort()

if (multicaSigningRequired && !multicaMissingSigningEnvironment.isEmpty()) {
    throw new GradleException(
        "Missing required Android release signing environment variables: " +
            multicaMissingSigningEnvironment.join(", ")
    )
}

if (multicaMissingSigningEnvironment.isEmpty()) {
    def multicaKeystoreFile =
        file(multicaSigningEnvironment.MULTICA_ANDROID_KEYSTORE_PATH)

    if (!multicaKeystoreFile.exists()) {
        throw new GradleException(
            "Android release keystore does not exist: " + multicaKeystoreFile
        )
    }

    android {
        signingConfigs {
            multicaRelease {
                storeFile multicaKeystoreFile
                storePassword multicaSigningEnvironment.MULTICA_ANDROID_KEYSTORE_PASSWORD
                keyAlias multicaSigningEnvironment.MULTICA_ANDROID_KEY_ALIAS
                keyPassword multicaSigningEnvironment.MULTICA_ANDROID_KEY_PASSWORD
            }
        }

        buildTypes {
            release {
                signingConfig signingConfigs.multicaRelease
            }
        }
    }
}
`;

export const ensureReleaseSigningApply = (contents: string) => {
  if (
    contents
      .split(/\r?\n/)
      .some((line) => line.trim() === RELEASE_SIGNING_APPLY)
  ) {
    return contents;
  }

  const separator = contents.endsWith("\n") ? "\n" : "\n\n";
  return `${contents}${separator}${RELEASE_SIGNING_APPLY}\n`;
};

const withAndroidReleaseSigning: ConfigPlugin = (config) => {
  const configWithBuildGradle = withAppBuildGradle(config, (gradleConfig) => {
    if (gradleConfig.modResults.language !== "groovy") {
      throw new Error("Android release signing requires a Groovy app build.gradle");
    }

    gradleConfig.modResults.contents = ensureReleaseSigningApply(
      gradleConfig.modResults.contents,
    );
    return gradleConfig;
  });

  return withDangerousMod(configWithBuildGradle, [
    "android",
    async (dangerousConfig) => {
      await writeFile(
        join(
          dangerousConfig.modRequest.platformProjectRoot,
          "app",
          "multica-release-signing.gradle",
        ),
        RELEASE_SIGNING_GRADLE,
        "utf8",
      );
      return dangerousConfig;
    },
  ]);
};

export default withAndroidReleaseSigning;
