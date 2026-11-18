declare module "react-test-renderer" {
  import type { ReactElement, ReactNode } from "react";

  export interface ReactTestInstance {
    type: any;
    props: Record<string, any>;
    parent: ReactTestInstance | null;
    children: Array<ReactTestInstance | string>;
    findByType(type: any): ReactTestInstance;
    findAllByType(type: any): ReactTestInstance[];
    findByProps(props: Record<string, any>): ReactTestInstance;
    findAllByProps(props: Record<string, any>): ReactTestInstance[];
    findAll(predicate: (node: ReactTestInstance) => boolean): ReactTestInstance[];
  }

  export interface ReactTestRenderer {
    root: ReactTestInstance;
    update(element: ReactElement): void;
    unmount(): void;
    toJSON(): any;
    toTree(): any;
    getInstance(): any;
  }

  export function create(element: ReactElement, options?: any): ReactTestRenderer;
  export function act(callback: () => void): void;
  export function act(callback: () => Promise<void>): Promise<void>;
}
