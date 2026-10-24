import { Checkbox } from "@/components/ui/checkbox";
import { $api, type Schemas } from "@/api/client";
import { RequestStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { useAreaOptions, useCategoryOptions } from "@/components/lookups";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useParamState } from "@/lib/params";
import { ago, dateTime, money } from "@/lib/format";
import { useMe } from "@/lib/access";

type Row = Schemas["ServiceRequestStaff"];

export function RequestQueuePage() {
  const me = useMe();
  const pager = useCursor();
  const [f, set] = useParamState(["status", "category_id", "service_area_id", "q", "from", "to", "mine"], pager.reset);
  const categories = useCategoryOptions();
  const areas = useAreaOptions();
  const areaName = new Map(areas.map((a) => [a.value, a.label]));
  const { data, isLoading, error } = $api.useQuery(
    "get",
    "/console/requests",
    {
      params: {
        query: {
          status: f.status as Row["status"] | undefined,
          category_id: f.category_id,
          service_area_id: f.service_area_id,
          q: f.q,
          from: f.from,
          to: f.to,
          mine: f.mine === "true" || undefined,
          cursor: pager.cursor,
        },
      },
    },
    // A live queue: other officers claim and assign work at the same time.
    { refetchInterval: 15_000 },
  );

  return (
    <>
      <PageHeader title="Request queue" description="Client requests waiting for triage, assignment or follow-up. Refreshes every 15 seconds." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={RequestStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="Category" value={f.category_id} options={categories} onChange={(v) => set("category_id", v)} />
        <EnumFilter label="Area" value={f.service_area_id} options={areas} onChange={(v) => set("service_area_id", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
        <TextFilter label="Search" value={f.q} placeholder="Reference or client" onChange={(v) => set("q", v)} />
        <label className="flex h-9 items-center gap-2 text-sm">
          <Checkbox checked={f.mine === "true"} onCheckedChange={(c) => set("mine", c ? "true" : undefined)} />
          Mine only
        </label>
      </FilterBar>
      <DataTable<Row>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(r) => r.id}
        rowHref={(r) => `/requests/${r.id}`}
        empty="No requests match these filters."
        columns={[
          { header: "Reference", cell: (r) => <span className="font-mono text-xs">{r.reference}</span> },
          { header: "Status", cell: (r) => <StatusBadge value={r.status} /> },
          {
            header: "Service",
            cell: (r) => (
              <div>
                <div>{r.service?.name}</div>
                <div className="text-muted-foreground text-xs">{r.category?.name}</div>
              </div>
            ),
          },
          { header: "Client", cell: (r) => r.client?.full_name ?? "—" },
          {
            header: "Area",
            cell: (r) => (
              <div>
                <div>{areaName.get(r.address?.service_area_id ?? "") ?? <span className="text-muted-foreground">Not in an area</span>}</div>
                {r.address?.estate && <div className="text-muted-foreground text-xs">{r.address.estate}</div>}
              </div>
            ),
          },
          { header: "Start", cell: (r) => dateTime(r.requested_start) },
          { header: "Total", cell: (r) => money(r.total_minor), className: "text-right tabular-nums" },
          { header: "Submitted", cell: (r) => ago(r.submitted_at) },
          {
            header: "Officer",
            cell: (r) =>
              r.triage_officer_id ? (r.triage_officer_id === me?.id ? "You" : "Claimed") : <span className="text-muted-foreground">Unclaimed</span>,
          },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
