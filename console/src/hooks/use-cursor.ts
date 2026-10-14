import { useCallback, useState } from "react";

/**
 * The API pages with opaque cursors (meta.next_cursor). Keeping the cursors we
 * have visited lets staff step back as well as forward.
 */
export function useCursor() {
  const [stack, setStack] = useState<string[]>([]);
  const cursor = stack.at(-1);
  return {
    cursor,
    page: stack.length + 1,
    next: (c: string) => setStack((s) => [...s, c]),
    prev: () => setStack((s) => s.slice(0, -1)),
    reset: useCallback(() => setStack([]), []),
  };
}
