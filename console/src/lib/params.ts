import { useSearchParams } from "react-router";

/**
 * Filter state kept in the URL, so a filtered queue can be bookmarked or shared
 * and the dashboard can deep-link into it. onChange resets paging.
 */
export function useParamState<K extends string>(keys: readonly K[], onChange?: () => void) {
  const [params, setParams] = useSearchParams();
  const values = Object.fromEntries(keys.map((k) => [k, params.get(k) ?? undefined])) as Record<K, string | undefined>;
  /** Sets several filters in one navigation, since two navigations in a row would lose the first. */
  function setMany(patch: Partial<Record<K, string | undefined>>) {
    setParams(
      (p) => {
        const next = new URLSearchParams(p);
        for (const [key, value] of Object.entries(patch) as [K, string | undefined][]) {
          if (value) next.set(key, value);
          else next.delete(key);
        }
        return next;
      },
      { replace: true },
    );
    onChange?.();
  }
  function set(key: K, value: string | undefined) {
    setMany({ [key]: value } as Partial<Record<K, string | undefined>>);
  }
  return [values, set, setMany] as const;
}
