import { useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { toast } from "sonner";

import { $api, errorMessage, type Schemas } from "@/api/client";
import { LeadStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useAreaName, useCategoryName } from "@/components/lookups";
import { DetailList, ErrorState, Loading, PageHeader, Section, TextBlock } from "@/components/page";
import { Pager } from "@/components/pager";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { useCursor } from "@/hooks/use-cursor";
import { useCan, useMe } from "@/lib/access";
import { dateTime, humanize } from "@/lib/format";
import { useParamState } from "@/lib/params";

type Lead = Schemas["Lead"];

/** What the visitor picked for the service, telling "Not sure yet" apart from nothing chosen. */
function useLeadLabels() {
  const categoryName = useCategoryName();
  const areaName = useAreaName();
  return {
    category: (l: Lead) => (l.category_id ? categoryName(l.category_id) : l.category_not_sure ? "Not sure yet" : "—"),
    area: (l: Lead) =>
      l.service_area_id ? areaName(l.service_area_id) : l.area_not_listed ? `Other: ${l.area_name || "not given"}` : "—",
  };
}

export function LeadsPage() {
  const pager = useCursor();
  const me = useMe();
  const [f, set] = useParamState(["status", "q", "mine", "from", "to"], pager.reset);
  const labels = useLeadLabels();
  const { data, isLoading, error } = $api.useQuery("get", "/console/leads", {
    params: {
      query: {
        status: f.status as Schemas["LeadStatus"] | undefined,
        q: f.q,
        assigned_to: f.mine ? me?.id : undefined,
        from: f.from,
        to: f.to,
        cursor: pager.cursor,
      },
    },
  });
  return (
    <>
      <PageHeader title="Leads" description="Enquiries from the public website. Open one to read it in full and follow it up." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={LeadStatus} onChange={(v) => set("status", v)} />
        <EnumFilter
          label="Owner"
          value={f.mine}
          allLabel="Anyone"
          options={[{ value: "1", label: "Assigned to me" }]}
          onChange={(v) => set("mine", v)}
        />
        <TextFilter label="Search" value={f.q} placeholder="Name, phone, email or message" onChange={(v) => set("q", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Lead>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(l) => l.id ?? `${l.full_name}-${l.created_at}`}
        rowHref={(l) => `/leads/${l.id}`}
        empty="No leads match these filters."
        columns={[
          { header: "Received", cell: (l) => dateTime(l.created_at) },
          {
            header: "Name",
            cell: (l) => (
              <div>
                <div>{l.full_name}</div>
                <div className="text-muted-foreground text-xs">{[l.phone, l.email].filter(Boolean).join(" · ") || "—"}</div>
              </div>
            ),
          },
          { header: "Status", cell: (l) => <StatusBadge value={l.status} /> },
          { header: "Service", cell: labels.category },
          { header: "Area", cell: labels.area },
          { header: "Message", cell: (l) => <span className="line-clamp-2 max-w-sm">{l.message || "—"}</span> },
          { header: "Owner", cell: (l) => l.assigned_to_name ?? "—" },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

export function LeadPage() {
  const { id = "" } = useParams();
  const path = { params: { path: { id } } };
  const me = useMe();
  const qc = useQueryClient();
  const labels = useLeadLabels();
  const q = $api.useQuery("get", "/console/leads/{id}", path);
  const canEdit = useCan("PATCH /console/leads/{id}");
  const canStaff = useCan("GET /console/staff");
  const canClients = useCan("GET /console/clients/{id}");
  const staff = $api.useQuery("get", "/console/staff", { params: { query: { status: "active" } } }, { enabled: canStaff });
  const update = $api.useMutation("patch", "/console/leads/{id}", {
    onSuccess: () => qc.invalidateQueries({ predicate: (x) => JSON.stringify(x.queryKey).includes("/console/leads") }),
  });
  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const l = q.data;

  async function patch(body: Schemas["LeadUpdate"], done: string) {
    try {
      await update.mutateAsync({ ...path, body });
      toast.success(done);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  const staffOptions = (staff.data ?? []).map((s) => ({ value: s.id, label: s.full_name }));
  return (
    <>
      <PageHeader
        back={{ to: "/leads", label: "Leads" }}
        title={
          <span className="flex flex-wrap items-center gap-3">
            {l.full_name} <StatusBadge value={l.status} />
          </span>
        }
        description={`Received ${dateTime(l.created_at)}${l.source ? ` from ${l.source}` : ""}`}
        actions={
          <Allowed when={canEdit}>
            {l.status === "new" && (
              <Button onClick={() => patch({ status: "contacted", ...(l.assigned_to ? {} : { assigned_to: me?.id }) }, "Marked as contacted")}>
                Mark contacted
              </Button>
            )}
            {l.assigned_to !== me?.id && (
              <Button variant="outline" onClick={() => patch({ assigned_to: me?.id }, "Assigned to you")}>
                Assign to me
              </Button>
            )}
            <FormDialog
              trigger="Update"
              title="Follow up this lead"
              fields={[
                { name: "status", label: "Status", type: "select", options: LeadStatus, required: true },
                ...(canStaff
                  ? [{ name: "assigned_to", label: "Owner", type: "select" as const, options: [{ value: "", label: "Nobody" }, ...staffOptions] }]
                  : []),
                { name: "notes", label: "Notes", type: "textarea", help: "What was said, next steps. Only staff see this." },
              ]}
              initial={{ status: l.status, assigned_to: l.assigned_to ?? "", notes: l.notes ?? "" }}
              submitLabel="Save"
              onSubmit={(body) => {
                const out: Schemas["LeadUpdate"] = { status: body.status as Schemas["LeadStatus"], notes: (body.notes as string) ?? "" };
                if (canStaff) out.assigned_to = (body.assigned_to as string) || null;
                return update.mutateAsync({ ...path, body: out });
              }}
            />
          </Allowed>
        }
      />
      <div className="grid gap-4 lg:grid-cols-3">
        <Section title="Enquiry" className="lg:col-span-2">
          <DetailList
            items={[
              ["Phone", l.phone ? <a className="text-primary hover:underline" href={`tel:${l.phone}`}>{l.phone}</a> : "—"],
              ["Email", l.email ? <a className="text-primary hover:underline" href={`mailto:${l.email}`}>{l.email}</a> : "—"],
              ["Service", labels.category(l)],
              ["Area", labels.area(l)],
            ]}
          />
          <div className="mt-4">
            <div className="text-muted-foreground mb-1 text-xs">Message</div>
            {l.message ? <TextBlock>{l.message}</TextBlock> : <div className="text-muted-foreground text-sm">No message.</div>}
          </div>
        </Section>
        <Section title="Follow-up">
          <DetailList
            items={[
              ["Status", humanize(l.status)],
              ["Owner", l.assigned_to_name ?? "Nobody yet"],
              ["First contacted", dateTime(l.contacted_at)],
              ["Last updated", dateTime(l.updated_at)],
              [
                "Client account",
                l.client_id ? (
                  canClients ? (
                    <Link className="text-primary hover:underline" to={`/clients/${l.client_id}`}>
                      {l.client_name ?? "Open client"}
                    </Link>
                  ) : (
                    (l.client_name ?? "Yes")
                  )
                ) : (
                  "None with this phone or email yet"
                ),
              ],
            ]}
          />
          {l.notes && (
            <div className="mt-4">
              <div className="text-muted-foreground mb-1 text-xs">Notes</div>
              <TextBlock>{l.notes}</TextBlock>
            </div>
          )}
        </Section>
      </div>
    </>
  );
}
