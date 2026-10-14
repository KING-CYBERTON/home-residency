import { useSearchParams } from "react-router";

/**
 * Filter state kept in the URL, so a filtered queue can be bookmarked or shared
 * and the dashboard can deep-link into it. onChange resets paging.
 */
export function useParamState<K extends string>(keys: readonly K[], onChange?: () => void) {
  const [params, setParams] = useSearchParams();
  const values = Object.fromEntries(keys.map((k) => [k, params.get(k) ?? undefined])) as Record<K, string | undefined>;
  function set(key: K, value: string | undefined) {
    setParams(
      (p) => {
        const next = new URLSearchParams(p);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
    onChange?.();
  }
  return [values, set] as const;
}
