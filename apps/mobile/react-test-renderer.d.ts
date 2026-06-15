declare module "react-test-renderer" {
  import type { ReactElement } from "react";

  interface TestRenderer {
    update(element: ReactElement): void;
    unmount(): void;
  }

  export function create(element: ReactElement): TestRenderer;
  export function act(callback: () => void | Promise<void>): Promise<void>;
}
