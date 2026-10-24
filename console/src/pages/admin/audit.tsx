import { XIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, FilterField, TextFilter } from "@/components/filters";
import { PersonFilter } from "@/components/person-filter";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { dateTime, humanize } from "@/lib/format";
import { useParamState } from "@/lib/params";
import { auditLabel } from "@/lib/audit";

/** The things the audit log records, and where each one's own page is in the console. */
const entityPages: Record<string, string | null> = {
  service_request: "/requests/",
  application: "/applications/",
  provider: "/providers/",
  client: "/clients/",
  complaint: "/complaints/",
  invoice: "/invoices/",
  lead: "/leads/",
  payout_batch: "/payout-batches/",
  document: null,
  user: null,
  app_setting: null,
  assignment: null,
  visit: null,
  payment: null,
  refund: null,
  payout: null,
  provider_earning: null,
  provider_contract: null,
  subscription: null,
  disciplinary_action: null,
  spot_check: null,
  training_course: null,
  training_enrolment: null,
  rate_card_entry: null,
  checklist_template: null,
  clearance_check: null,
  referee: null,
  variation: null,
};

function Entity({ e, onFilter }: { e: Schemas["AuditEntry"]; onFilter: () => void }) {
  const base = entityPages[e.entity_type];
  const id = e.entity_id;
  return (
    <div>
      <div>{humanize(e.entity_type)}</div>
      {id && (
        <div className="flex gap-2 text-xs">
          {base ? (
            <Link className="text-primary underline-offset-2 hover:underline" to={base + id}>
              Open
            </Link>
          ) : null}
          <button type="button" className="text-muted-foreground hover:text-foreground underline-offset-2 hover:underline" onClick={onFilter}>
            History
          </button>
        </div>
      )}
    </div>
  );
}

export function AuditLogPage() {
  const pager = useCursor();
  const [f, set, setMany] = useParamState(["entity_type", "entity_id", "actor_id", "action", "from", "to"], pager.reset);
  const [open, setOpen] = useState<number | null>(null);
  const { data, isLoading, error } = $api.useQuery("get", "/console/audit-log", {
    params: { query: { ...f, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader title="Audit log" description="Append-only record of every change, who made it and why." />
      <FilterBar>
        <EnumFilter label="Record" value={f.entity_type} options={Object.keys(entityPages).sort()} onChange={(v) => set("entity_type", v)} />
        {f.entity_id && (
          <FilterField label="One record">
            <span className="border-input flex h-9 items-center gap-2 rounded-md border px-3 text-sm">
              {humanize(f.entity_type ?? "record")} history
              <button type="button" className="text-muted-foreground hover:text-foreground ml-auto" aria-label="Clear record" onClick={() => set("entity_id", undefined)}>
                <XIcon className="size-4" />
              </button>
            </span>
          </FilterField>
        )}
        <PersonFilter label="Done by" kind="staff" value={f.actor_id} onChange={(v) => set("actor_id", v)} />
        <TextFilter label="Action" value={f.action} placeholder="e.g. provider.suspend" onChange={(v) => set("action", v)} />
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
          {
            header: "Record",
            cell: (e) => (
              <Entity
                e={e}
                onFilter={() => setMany({ entity_type: e.entity_type, entity_id: e.entity_id ?? undefined })}
              />
            ),
          },
          {
            header: "Done by",
            cell: (e) => (
              <div>
                <div>{e.actor_name ?? (e.actor_kind ? humanize(e.actor_kind) : "System")}</div>
                {e.actor_name && e.actor_kind && <div className="text-muted-foreground text-xs">{humanize(e.actor_kind)}</div>}
              </div>
            ),
          },
          { header: "Reason", cell: (e) => <span className="line-clamp-2 max-w-xs text-xs">{e.reason}</span> },
          {
            header: "From",
            cell: (e) => (
              <div className="max-w-48 text-xs">
                <div>{e.ip_address ?? "—"}</div>
                {e.user_agent && (
                  <div className="text-muted-foreground truncate" title={e.user_agent}>
                    {browserName(e.user_agent)}
                  </div>
                )}
              </div>
            ),
          },
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

/** A short name for the browser and system in a user agent, such as "Chrome on Windows". */
function browserName(ua: string): string {
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Chrome\//.test(ua)
      ? "Chrome"
      : /Firefox\//.test(ua)
        ? "Firefox"
        : /Safari\//.test(ua)
          ? "Safari"
          : /Dart|okhttp/i.test(ua)
            ? "Homend app"
            : null;
  const os = /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Mac OS X/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : null;
  if (browser && os) return `${browser} on ${os}`;
  return browser ?? os ?? ua.slice(0, 40);
}
