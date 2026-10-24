import { useNavigate } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { ComplaintSeverity, ComplaintSource, ComplaintStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan } from "@/lib/access";
import { ago } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function ComplaintsPage() {
  const navigate = useNavigate();
  const pager = useCursor();
  const [f, set] = useParamState(
    ["status", "severity", "provider_id", "from", "to", "new", "request_id", "client_id"],
    pager.reset,
  );
  const create = $api.useMutation("post", "/console/complaints");
  const canCreate = useCan("POST /console/complaints");
  const { data, isLoading, error } = $api.useQuery(
    "get",
    "/console/complaints",
    {
      params: {
        query: {
          status: f.status as Schemas["ComplaintStatus"] | undefined,
          severity: f.severity as Schemas["ComplaintSeverity"] | undefined,
          provider_id: f.provider_id,
          from: f.from,
          to: f.to,
          cursor: pager.cursor,
        },
      },
    },
    { refetchInterval: 30_000 },
  );
  return (
    <>
      <PageHeader
        title="Complaints"
        description="Safeguarding and loss complaints suspend the provider straight away. A provider must be heard before a decision."
        actions={
          <Allowed when={canCreate}>
            <FormDialog
              trigger="Open complaint"
              triggerVariant="default"
              title="Open a complaint"
              description="For complaints phoned in, raised by a supervisor or by management."
              autoOpen={f.new === "1"}
              fields={[
                { name: "severity", label: "Severity", type: "select", options: ComplaintSeverity, required: true },
                { name: "source", label: "Source", type: "select", options: ComplaintSource },
                { name: "summary", label: "Summary", required: true },
                { name: "details", label: "Details", type: "textarea" },
                { name: "request_id", label: "Request ID" },
                { name: "client_id", label: "Client", type: "person", person: "client" },
                { name: "provider_id", label: "Provider", type: "person", person: "provider" },
              ]}
              initial={{ source: "management", request_id: f.request_id, client_id: f.client_id, provider_id: f.provider_id }}
              onSubmit={(body) => create.mutateAsync({ body })}
              onDone={(c) => navigate(`/complaints/${(c as Schemas["ComplaintStaff"]).id}`)}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={ComplaintStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="Severity" value={f.severity} options={ComplaintSeverity} onChange={(v) => set("severity", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Schemas["ComplaintStaff"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(c) => c.id}
        rowHref={(c) => `/complaints/${c.id}`}
        empty="No complaints match these filters."
        rowClassName={(c) => (c.severity === "safeguarding" && c.status !== "closed" ? "bg-destructive/5" : undefined)}
        columns={[
          { header: "Reference", cell: (c) => <span className="font-mono text-xs">{c.reference}</span> },
          { header: "Severity", cell: (c) => <StatusBadge value={c.severity} /> },
          { header: "Status", cell: (c) => <StatusBadge value={c.status} /> },
          { header: "Summary", cell: (c) => <span className="line-clamp-2 max-w-md">{c.summary}</span> },
          { header: "Provider", cell: (c) => c.provider?.full_name ?? "—" },
          { header: "Client", cell: (c) => c.client?.full_name ?? "—" },
          { header: "Opened", cell: (c) => ago(c.created_at) },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
