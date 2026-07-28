// @vitest-environment jsdom
import { useLayoutEffect, useMemo, useRef, type ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enEditor from "../locales/en/editor.json";
import type { ContentEditorRef } from "./content-editor";
import type { UploadGate } from "./use-upload-gate";
import {
  useCoordinatedUploads,
  __liveEditorRegistryKeysForTest,
  type UploadDraftBinding,
} from "./use-coordinated-uploads";

const TEST_RESOURCES = { en: { common: enCommon, editor: enEditor } };

const inertGate: UploadGate = {
  uploading: false,
  onUploadingChange: () => {},
  isBlocked: () => false,
};

function makeBinding(key: string): UploadDraftBinding {
  return {
    registryKey: key,
    getUploads: () => [],
    addUpload: () => {},
    settleUpload: () => {},
    failUpload: () => {},
    removeUpload: () => {},
    getBody: () => "",
    appendToBody: () => {},
  };
}

function HookHost({ registryKey }: { registryKey: string }) {
  const editorRef = useRef<ContentEditorRef | null>(null);
  const binding = useMemo(() => makeBinding(registryKey), [registryKey]);
  useCoordinatedUploads(binding, [], {}, inertGate, editorRef);
  return null;
}

/**
 * Captures the registry from a PARENT layout effect: layout effects run
 * child-first within a commit, so at capture time the hook's registration for
 * this commit must already be visible. A passive registration would not be —
 * it flushes a task later, which is exactly the settle window where a
 * write-back for the old key can insert into an editor already holding the
 * new draft's document (chat's pinned-switch adopts in layout).
 */
function CaptureAfterCommit({
  registryKey,
  capture,
  children,
}: {
  registryKey: string;
  capture: (keys: string[]) => void;
  children: ReactNode;
}) {
  useLayoutEffect(() => {
    capture(__liveEditorRegistryKeysForTest());
  }, [registryKey, capture]);
  return <>{children}</>;
}

function Probe({ registryKey, capture }: { registryKey: string; capture: (keys: string[]) => void }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CaptureAfterCommit registryKey={registryKey} capture={capture}>
        <HookHost registryKey={registryKey} />
      </CaptureAfterCommit>
    </I18nProvider>
  );
}

describe("useCoordinatedUploads live-editor registry timing", () => {
  it("registers within the commit (layout), so a key switch is visible before any task runs", () => {
    const captures: string[][] = [];
    const capture = (keys: string[]) => captures.push(keys);

    const view = render(<Probe registryKey="probe:a" capture={capture} />);
    expect(captures.at(-1)).toContain("probe:a");

    view.rerender(<Probe registryKey="probe:b" capture={capture} />);

    // At the parent's layout effect of the SWITCH commit, the registry must
    // already point at the new key and no longer at the old one. A passive
    // registration fails both assertions here.
    expect(captures.at(-1)).toContain("probe:b");
    expect(captures.at(-1)).not.toContain("probe:a");

    view.unmount();
    expect(__liveEditorRegistryKeysForTest()).not.toContain("probe:b");
  });
});
