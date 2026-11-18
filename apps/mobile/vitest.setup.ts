import { vi } from "vitest";

// expo-secure-store initialises a native EventEmitter at import, which does not
// exist under Node. Component and route suites reach it transitively (auth,
// workspace and preference stores), so give every suite an in-memory stand-in.
// A suite that mocks the module itself still takes precedence.
vi.mock("expo-secure-store", () => {
  const values = new Map<string, string>();
  return {
    getItemAsync: vi.fn(async (key: string) => values.get(key) ?? null),
    setItemAsync: vi.fn(async (key: string, value: string) => {
      values.set(key, value);
    }),
    deleteItemAsync: vi.fn(async (key: string) => {
      values.delete(key);
    }),
    getItem: vi.fn((key: string) => values.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => {
      values.set(key, value);
    }),
  };
});

// expo-localization has the same native-module dependency. English keeps the
// real i18n resources loadable so rendered labels match the source strings.
vi.mock("expo-localization", () => ({
  getLocales: () => [{ languageTag: "en-US", languageCode: "en" }],
  getCalendars: () => [{ timeZone: "UTC" }],
}));

// Suites render screens without MobileI18nProvider, so useT would read an
// uninitialised i18next and return raw keys. Bind it to the initialised
// English singleton instead; suites that mock "@/lib/i18n" still override this.
vi.mock("@/lib/i18n", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/i18n")>();
  return {
    ...actual,
    useT: (ns?: string) => ({
      t: actual.i18n.getFixedT("en", ns ?? "common"),
      i18n: actual.i18n,
    }),
  };
});
