import { $api, type Schemas } from "@/api/client";
import { ProviderStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { useAreaName, useAreaOptions, useCategoryOptions } from "@/components/lookups";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { score } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function ProvidersPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status", "category_id", "service_area_id", "q"], pager.reset);
  const categories = useCategoryOptions();
  const areas = useAreaOptions();
  const areaName = useAreaName();
  const { data, isLoading, error } = $api.useQuery("get", "/console/providers", {
    params: {
      query: {
        status: f.status as Schemas["ProviderStatus"] | undefined,
        category_id: f.category_id,
        service_area_id: f.service_area_id,
        q: f.q,
        cursor: pager.cursor,
      },
    },
  });
  return (
    <>
      <PageHeader title="Providers" description="Domestic workers and service providers at every stage, from applicant to active." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={ProviderStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="Category" value={f.category_id} options={categories} onChange={(v) => set("category_id", v)} />
        <EnumFilter label="Area" value={f.service_area_id} options={areas} onChange={(v) => set("service_area_id", v)} />
        <TextFilter label="Search" value={f.q} placeholder="Name or phone" onChange={(v) => set("q", v)} />
      </FilterBar>
      <DataTable<Schemas["ProviderSummary"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(p) => p.id}
        rowHref={(p) => `/providers/${p.id}`}
        empty="No providers match these filters."
        columns={[
          { header: "Name", cell: (p) => p.full_name },
          { header: "Status", cell: (p) => <StatusBadge value={p.status} /> },
          { header: "Phone", cell: (p) => p.phone ?? "—" },
          { header: "Home area", cell: (p) => areaName(p.home_service_area_id) },
          { header: "Rating", cell: (p) => score(p.rating_avg), className: "tabular-nums" },
          { header: "Appraisal", cell: (p) => score(p.appraisal_score), className: "tabular-nums" },
          { header: "Jobs", cell: (p) => p.completed_jobs ?? 0, className: "tabular-nums" },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
