import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { ExceptionKind, VariationStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useParamState } from "@/lib/params";
import { ago, dateTime, humanize, money } from "@/lib/format";

import { ExceptionResolveButton, VariationDecisionButtons } from "./shared";

export function VariationsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status"], pager.reset);
  const status = (f.status ?? "pending") as Schemas["VariationStatus"];
  const { data, isLoading, error } = $api.useQuery("get", "/console/variations", {
    params: { query: { status, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader title="Variations" description="Changes to agreed scope or price raised during a job." />
      <FilterBar>
        <EnumFilter label="Status" value={status} options={VariationStatus} onChange={(v) => set("status", v)} />
      </FilterBar>
      <DataTable<Schemas["Variation"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(v) => v.id}
        empty="No variations."
        columns={[
          { header: "Request", cell: (v) => <Link className="text-primary hover:underline" to={`/requests/${v.request_id}`}>Open</Link> },
          { header: "Description", cell: (v) => v.description },
          { header: "Raised by", cell: (v) => humanize(v.raised_by) },
          { header: "Client change", cell: (v) => money(v.client_delta_minor), className: "text-right tabular-nums" },
          { header: "Provider change", cell: (v) => money(v.provider_delta_minor), className: "text-right tabular-nums" },
          { header: "Status", cell: (v) => <StatusBadge value={v.status} /> },
          { header: "Raised", cell: (v) => ago(v.created_at) },
          { header: "", cell: (v) => v.status === "pending" && <div className="flex justify-end gap-1"><VariationDecisionButtons id={v.id} /></div> },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

export function ExceptionsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["kind", "open", "from", "to"], pager.reset);
  const open = f.open === undefined ? true : f.open === "true" ? true : f.open === "false" ? false : undefined;
  const { data, isLoading, error } = $api.useQuery(
    "get",
    "/console/exceptions",
    { params: { query: { kind: f.kind as Schemas["ExceptionKind"] | undefined, open, from: f.from, to: f.to, cursor: pager.cursor } } },
    { refetchInterval: 30_000 },
  );
  return (
    <>
      <PageHeader title="Daily exception log" description="Late arrivals, no-shows, expired offers, low ratings and other things that need a look." />
      <FilterBar>
        <EnumFilter label="Kind" value={f.kind} options={ExceptionKind} onChange={(v) => set("kind", v)} />
        <EnumFilter
          label="State"
          value={f.open ?? "true"}
          options={[
            { value: "true", label: "Open" },
            { value: "false", label: "Resolved" },
            { value: "all", label: "All" },
          ]}
          allLabel="Open"
          onChange={(v) => set("open", v)}
        />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Schemas["JobException"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(x) => x.id}
        empty="No exceptions."
        columns={[
          { header: "Kind", cell: (x) => <StatusBadge value={x.kind} /> },
          { header: "Detected", cell: (x) => dateTime(x.detected_at) },
          { header: "Request", cell: (x) => (x.request_id ? <Link className="text-primary hover:underline" to={`/requests/${x.request_id}`}>Open</Link> : "—") },
          { header: "Provider", cell: (x) => (x.provider_id ? <Link className="text-primary hover:underline" to={`/providers/${x.provider_id}`}>Open</Link> : "—") },
          { header: "Details", cell: (x) => <code className="text-xs">{x.details ? JSON.stringify(x.details) : ""}</code> },
          { header: "Resolution", cell: (x) => x.resolution ?? "—" },
          { header: "", cell: (x) => !x.reviewed_at && <ExceptionResolveButton id={x.id} /> },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
