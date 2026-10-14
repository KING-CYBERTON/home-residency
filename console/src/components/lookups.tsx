import { $api } from "@/api/client";
import { shortId } from "@/lib/format";

/** Reference lists that many screens need to turn ids into names. Cached for the session. */
const staticOpts = { staleTime: 5 * 60_000 };

export function useCategories() {
  return $api.useQuery("get", "/console/categories", {}, staticOpts);
}

export function useServiceAreas() {
  return $api.useQuery("get", "/console/service-areas", {}, staticOpts);
}

export function useServices() {
  return $api.useQuery("get", "/console/services", {}, staticOpts);
}

function nameMap(list: { id: string; name: string }[] | undefined) {
  return new Map((list ?? []).map((x) => [x.id, x.name]));
}

export function useCategoryName() {
  const { data } = useCategories();
  const map = nameMap(data);
  return (id: string | null | undefined) => (id ? (map.get(id) ?? shortId(id)) : "—");
}

export function useAreaName() {
  const { data } = useServiceAreas();
  const map = nameMap(data);
  return (id: string | null | undefined) => (id ? (map.get(id) ?? shortId(id)) : "—");
}

export function useServiceName() {
  const { data } = useServices();
  const map = nameMap(data);
  return (id: string | null | undefined) => (id ? (map.get(id) ?? shortId(id)) : "—");
}

export function useCategoryOptions() {
  const { data } = useCategories();
  return (data ?? []).map((c) => ({ value: c.id, label: c.name }));
}

export function useAreaOptions() {
  const { data } = useServiceAreas();
  return (data ?? []).map((a) => ({ value: a.id, label: a.name }));
}

export function useServiceOptions() {
  const { data } = useServices();
  return (data ?? []).map((s) => ({ value: s.id, label: s.name }));
}
