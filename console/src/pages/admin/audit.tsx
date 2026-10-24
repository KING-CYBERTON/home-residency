import { useState } from "react";

import { $api, type Schemas } from "@/api/client";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/data-table";
import { FilterBar, TextFilter } from "@/components/filters";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { dateTime, humanize } from "@/lib/format";
import { useParamState } from "@/lib/params";
import { auditLabel } from "@/lib/audit";

export function AuditLogPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["entity_type", "entity_id", "actor_id", "action", "from", "to"], pager.reset);
  const [open, setOpen] = useState<number | null>(null);
  const { data, isLoading, error } = $api.useQuery("get", "/console/audit-log", {
    params: { query: { ...f, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader title="Audit log" description="Append-only record of every change, who made it and why." />
      <FilterBar>
        <TextFilter label="Entity type" value={f.entity_type} placeholder="provider" onChange={(v) => set("entity_type", v)} />
        <TextFilter label="Entity ID" value={f.entity_id} onChange={(v) => set("entity_id", v)} />
        <TextFilter label="Actor ID" value={f.actor_id} onChange={(v) => set("actor_id", v)} />
        <TextFilter label="Action" value={f.action} placeholder="provider.suspend" onChange={(v) => set("action", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Schemas["AuditEntry"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(e) => e.id}
        empty="No entries match these filters."
        columns={[
          { header: "When", cell: (e) => dateTime(e.occurred_at) },
          {
            header: "Action",
            cell: (e) => (
              <div>
                <div>{auditLabel(e.action)}</div>
                <code className="text-muted-foreground text-xs">{e.action}</code>
              </div>
            ),
          },
          { header: "Entity", cell: (e) => <span>{humanize(e.entity_type)} <code className="text-muted-foreground text-xs">{e.entity_id?.slice(0, 8)}</code></span> },
          { header: "Actor", cell: (e) => <span>{e.actor_kind ? humanize(e.actor_kind) : "System"} <code className="text-muted-foreground text-xs">{e.actor_id?.slice(0, 8)}</code></span> },
          { header: "Reason", cell: (e) => <span className="line-clamp-2 max-w-xs text-xs">{e.reason}</span> },
          { header: "IP", cell: (e) => <span className="text-xs">{e.ip_address}</span> },
          {
            header: "",
            cell: (e) =>
              (e.before_state || e.after_state) && (
                <div className="text-right">
                  <Button variant="ghost" size="sm" onClick={() => setOpen(open === e.id ? null : e.id)}>
                    {open === e.id ? "Hide" : "Changes"}
                  </Button>
                  {open === e.id && (
                    <div className="mt-2 grid gap-2 text-left text-xs md:grid-cols-2">
                      <pre className="bg-muted/50 max-w-md overflow-x-auto rounded p-2">{JSON.stringify(e.before_state, null, 2)}</pre>
                      <pre className="bg-muted/50 max-w-md overflow-x-auto rounded p-2">{JSON.stringify(e.after_state, null, 2)}</pre>
                    </div>
                  )}
                </div>
              ),
          },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
