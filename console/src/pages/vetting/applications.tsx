import { $api, type Schemas } from "@/api/client";
import { ApplicationStatus } from "@/api/enums.gen";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { useCategoryName, useCategoryOptions } from "@/components/lookups";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useMe } from "@/lib/access";
import { ago } from "@/lib/format";
import { useParamState } from "@/lib/params";

type Row = Schemas["ApplicationSummary"];

export function ApplicationsPage() {
  const me = useMe();
  const pager = useCursor();
  const [f, set] = useParamState(["status", "category_id", "q", "reverification", "mine"], pager.reset);
  const categories = useCategoryOptions();
  const categoryName = useCategoryName();
  const { data, isLoading, error } = $api.useQuery(
    "get",
    "/console/applications",
    {
      params: {
        query: {
          status: f.status as Row["status"] | undefined,
          category_id: f.category_id,
          q: f.q,
          reverification: f.reverification === "true" || undefined,
          vetting_officer_id: f.mine === "true" ? me?.id : undefined,
          cursor: pager.cursor,
        },
      },
    },
    { refetchInterval: 30_000 },
  );
  return (
    <>
      <PageHeader title="Applications" description="Provider applications moving through vetting and approval." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={ApplicationStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="Category" value={f.category_id} options={categories} onChange={(v) => set("category_id", v)} />
        <TextFilter label="Search" value={f.q} placeholder="Reference or name" onChange={(v) => set("q", v)} />
        <label className="flex h-9 items-center gap-2 text-sm">
          <Checkbox checked={f.mine === "true"} onCheckedChange={(c) => set("mine", c ? "true" : undefined)} />
          Mine only
        </label>
        <label className="flex h-9 items-center gap-2 text-sm">
          <Checkbox checked={f.reverification === "true"} onCheckedChange={(c) => set("reverification", c ? "true" : undefined)} />
          Re-verifications
        </label>
      </FilterBar>
      <DataTable<Row>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(r) => r.id}
        rowHref={(r) => `/applications/${r.id}`}
        empty="No applications match these filters."
        columns={[
          { header: "Reference", cell: (r) => <span className="font-mono text-xs">{r.reference}</span> },
          {
            header: "Applicant",
            cell: (r) => (
              <span className="flex items-center gap-2">
                {r.provider_name ?? "—"}
                {r.is_reverification && <Badge variant="info">Re-verification</Badge>}
              </span>
            ),
          },
          { header: "Category", cell: (r) => categoryName(r.category_id) },
          { header: "Status", cell: (r) => <StatusBadge value={r.status} /> },
          {
            header: "Checklist",
            cell: (r) =>
              r.checklist_progress ? (
                <Progress done={r.checklist_progress.satisfied ?? 0} total={r.checklist_progress.total ?? 0} />
              ) : (
                "—"
              ),
          },
          { header: "Submitted", cell: (r) => ago(r.submitted_at) },
          {
            header: "Officer",
            cell: (r) => (r.vetting_officer_id ? (r.vetting_officer_id === me?.id ? "You" : "Claimed") : <span className="text-muted-foreground">Unclaimed</span>),
          },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

export function Progress({ done, total }: { done: number; total: number }) {
  const pct = total ? Math.round((done / total) * 100) : 0;
  return (
    <div className="flex items-center gap-2">
      <div className="bg-muted h-1.5 w-20 overflow-hidden rounded-full">
        <div className="bg-primary h-full" style={{ width: `${pct}%` }} />
      </div>
      <span className="text-muted-foreground text-xs tabular-nums">
        {done}/{total}
      </span>
    </div>
  );
}
