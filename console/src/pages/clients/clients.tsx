import { Link, useParams } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { AccountStatus, IdVerificationStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useAreaName } from "@/components/lookups";
import { DetailList, ErrorState, Loading, PageHeader, Section, TextBlock } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan } from "@/lib/access";
import { date, dateTime, money } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function ClientsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status", "id_verification", "q"], pager.reset);
  const { data, isLoading, error } = $api.useQuery("get", "/console/clients", {
    params: {
      query: {
        status: f.status as Schemas["AccountStatus"] | undefined,
        id_verification: f.id_verification as Schemas["IdVerificationStatus"] | undefined,
        q: f.q,
        cursor: pager.cursor,
      },
    },
  });
  return (
    <>
      <PageHeader title="Clients" description="Households using the platform." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={AccountStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="ID verification" value={f.id_verification} options={IdVerificationStatus} onChange={(v) => set("id_verification", v)} />
        <TextFilter label="Search" value={f.q} placeholder="Name or phone" onChange={(v) => set("q", v)} />
      </FilterBar>
      <DataTable<Schemas["ClientSummary"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(c) => c.id}
        rowHref={(c) => `/clients/${c.id}`}
        empty="No clients match these filters."
        columns={[
          { header: "Name", cell: (c) => c.full_name },
          { header: "Phone", cell: (c) => c.phone ?? "—" },
          { header: "Status", cell: (c) => <StatusBadge value={c.status} /> },
          { header: "ID", cell: (c) => <StatusBadge value={c.id_verification} /> },
          { header: "Joined", cell: (c) => date(c.created_at) },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

export function ClientPage() {
  const { id = "" } = useParams();
  const path = { params: { path: { id } } };
  const q = $api.useQuery("get", "/console/clients/{id}", path);
  const areaName = useAreaName();
  const idDecision = $api.useMutation("post", "/console/clients/{id}/id-decision");
  const suspend = $api.useMutation("post", "/console/clients/{id}/suspend");
  const reinstate = $api.useMutation("post", "/console/clients/{id}/reinstate");
  const can = {
    id: useCan("POST /console/clients/{id}/id-decision"),
    suspend: useCan("POST /console/clients/{id}/suspend"),
    reinstate: useCan("POST /console/clients/{id}/reinstate"),
    requests: useCan("GET /console/requests"),
    invoices: useCan("GET /console/invoices"),
  };
  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const c = q.data;
  return (
    <>
      <PageHeader
        back={{ to: "/clients", label: "Clients" }}
        title={
          <span className="flex flex-wrap items-center gap-3">
            {c.full_name} <StatusBadge value={c.status} />
          </span>
        }
        description={[c.phone, c.email].filter(Boolean).join(" · ")}
        actions={
          <>
            <Allowed when={can.id && c.id_verification === "pending"}>
              <FormDialog
                trigger="ID decision"
                triggerVariant="default"
                title="Verify or reject the client's ID"
                description="Required before booking categories such as live-in work."
                fields={[
                  { name: "decision", label: "Decision", type: "select", options: ["verified", "rejected"], required: true },
                  { name: "reason", label: "Reason", type: "textarea", show: (s) => s.decision === "rejected" },
                ]}
                onSubmit={(body) => idDecision.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.suspend && c.status === "active"}>
              <FormDialog
                trigger="Suspend"
                triggerVariant="destructive"
                title="Suspend client account"
                fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                destructive
                onSubmit={(body) => suspend.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.reinstate && c.status === "suspended"}>
              <FormDialog
                trigger="Reinstate"
                title="Reinstate client account"
                fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                onSubmit={(body) => reinstate.mutateAsync({ ...path, body })}
              />
            </Allowed>
          </>
        }
      />
      <div className="grid gap-4 lg:grid-cols-3">
        <Section title="Account" className="lg:col-span-2">
          <DetailList
            items={[
              ["ID verification", <StatusBadge value={c.id_verification} />],
              ["Joined", dateTime(c.created_at)],
              ["Requests", can.requests ? <Link className="text-primary hover:underline" to={`/requests?q=${encodeURIComponent(c.phone ?? c.full_name)}`}>{c.request_count ?? 0}</Link> : (c.request_count ?? 0)],
              ["Complaints", c.complaint_count ?? 0],
              ["Subscription", c.subscription ? `${c.subscription.plan.name} (${c.subscription.status})` : "None"],
              ["Subscription price", c.subscription ? money(c.subscription.plan.price_minor) : "—"],
              ["Invoices", can.invoices ? <Link className="text-primary hover:underline" to={`/invoices?client_id=${c.id}`}>View invoices</Link> : "—"],
            ]}
          />
          {c.household_notes && (
            <div className="mt-4">
              <div className="text-muted-foreground mb-1 text-xs">Household notes</div>
              <TextBlock>{c.household_notes}</TextBlock>
            </div>
          )}
        </Section>
        <Section title="Addresses">
          {c.addresses?.length ? (
            <ul className="space-y-3 text-sm">
              {c.addresses.map((a) => (
                <li key={a.id}>
                  <div className="font-medium">
                    {a.label} {a.is_default && <span className="text-muted-foreground text-xs">(default)</span>}
                  </div>
                  <div className="text-muted-foreground">
                    {[a.house_number, a.street, a.estate, areaName(a.service_area_id)].filter((x) => x && x !== "—").join(", ")}
                  </div>
                  {a.landmark && <div className="text-muted-foreground text-xs">Near {a.landmark}</div>}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground text-sm">No addresses.</p>
          )}
        </Section>
      </div>
    </>
  );
}
