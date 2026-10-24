import { useState } from "react";
import { Link, useParams } from "react-router";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { $api, type Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useAreaOptions } from "@/components/lookups";
import { DetailList, Empty, ErrorState, Loading, PageHeader, Section, TextBlock } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { useCan, useMe } from "@/lib/access";
import { dateTime, humanize, money, score } from "@/lib/format";

import { ExceptionResolveButton, VariationDecisionButtons } from "./shared";

type Shortlist = Schemas["ShortlistEntry"];

const OPEN_STATUSES = ["submitted", "triaging", "awaiting_client_choice", "awaiting_provider"];

export function RequestPage() {
  const { id = "" } = useParams();
  const me = useMe();
  const areas = useAreaOptions();
  const path = { params: { path: { id } } };
  const req = $api.useQuery("get", "/console/requests/{id}", path, { refetchInterval: 15_000 });
  const canShortlist = useCan("GET /console/requests/{id}/shortlist");
  const shortlist = $api.useQuery("get", "/console/requests/{id}/shortlist", path, { enabled: canShortlist });

  const claim = $api.useMutation("post", "/console/requests/{id}/claim");
  const generate = $api.useMutation("post", "/console/requests/{id}/shortlist");
  const share = $api.useMutation("post", "/console/requests/{id}/share-shortlist");
  const assign = $api.useMutation("post", "/console/requests/{id}/assignments");
  const withdraw = $api.useMutation("post", "/console/assignments/{id}/withdraw");
  const schedule = $api.useMutation("post", "/console/requests/{id}/visits");
  const updateVisit = $api.useMutation("patch", "/console/visits/{id}");
  const manualCheckIn = $api.useMutation("post", "/console/visits/{id}/manual-check-in");
  const unfilled = $api.useMutation("post", "/console/requests/{id}/mark-unfilled");
  const cancel = $api.useMutation("post", "/console/requests/{id}/cancel");

  const can = {
    claim: useCan("POST /console/requests/{id}/claim"),
    shortlist: useCan("POST /console/requests/{id}/shortlist"),
    share: useCan("POST /console/requests/{id}/share-shortlist"),
    assign: useCan("POST /console/requests/{id}/assignments"),
    withdraw: useCan("POST /console/assignments/{id}/withdraw"),
    schedule: useCan("POST /console/requests/{id}/visits"),
    updateVisit: useCan("PATCH /console/visits/{id}"),
    checkIn: useCan("POST /console/visits/{id}/manual-check-in"),
    unfilled: useCan("POST /console/requests/{id}/mark-unfilled"),
    cancel: useCan("POST /console/requests/{id}/cancel"),
    complaint: useCan("POST /console/complaints"),
  };

  const [selected, setSelected] = useState<string[]>([]);
  const [comparing, setComparing] = useState(false);

  if (req.isLoading) return <Loading />;
  if (req.error || !req.data) return <ErrorState error={req.error} />;
  const r = req.data;
  const open = OPEN_STATUSES.includes(r.status);
  const mine = r.triage_officer_id === me?.id;
  const entries = shortlist.data ?? [];
  const compared = entries.filter((e) => selected.includes(e.id));

  return (
    <>
      <PageHeader
        back={{ to: "/requests", label: "Request queue" }}
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{r.reference}</span>
            <StatusBadge value={r.status} />
            {r.priority ? <Badge variant="warning">Priority {r.priority}</Badge> : null}
          </span>
        }
        description={`${r.service?.name ?? ""} · ${r.category?.name ?? ""} · submitted ${dateTime(r.submitted_at)}`}
        actions={
          <>
            <Allowed when={can.claim && open && !mine}>
              <FormDialog
                trigger={r.triage_officer_id ? "Take over" : "Claim"}
                triggerVariant="default"
                title="Claim this request"
                description="You become the triage officer responsible for it."
                submitLabel="Claim"
                success="Request claimed"
                onSubmit={() => claim.mutateAsync(path)}
              />
            </Allowed>
            <Allowed when={can.complaint}>
              <Button variant="outline" size="sm" asChild>
                <Link to={`/complaints?new=1&request_id=${r.id}${r.client ? `&client_id=${r.client.id}` : ""}`}>Open complaint</Link>
              </Button>
            </Allowed>
            <Allowed when={can.unfilled && open}>
              <FormDialog
                trigger="Mark unfilled"
                title="Mark as unfilled"
                description="No suitable provider is available. The client is told."
                fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                destructive
                onSubmit={(body) => unfilled.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.cancel && !["closed", "cancelled", "unfilled"].includes(r.status)}>
              <FormDialog
                trigger="Cancel"
                title="Cancel request"
                fields={[
                  { name: "cancelled_by", label: "Cancelled by", type: "select", options: ["client", "provider", "management"], required: true },
                  { name: "reason", label: "Reason", type: "textarea", required: true },
                  { name: "waive_fee", label: "Waive the cancellation fee", type: "checkbox" },
                ]}
                initial={{ cancelled_by: "client" }}
                destructive
                submitLabel="Cancel request"
                onSubmit={(body) => cancel.mutateAsync({ ...path, body })}
              />
            </Allowed>
          </>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <Section title="Request" className="lg:col-span-2">
          <DetailList
            items={[
              ["Service", r.service?.name],
              ["Quantity", r.quantity ?? "—"],
              ["Requested start", dateTime(r.requested_start)],
              ["Requested end", dateTime(r.requested_end)],
              ["Recurrence", r.recurrence_rule ?? "One-off"],
              ["Client may choose", r.offer_client_choice ? "Yes" : "No"],
              ["Client price", money(r.total_minor)],
              ["Provider pay", money(r.provider_pay_minor)],
              ["Placement fee", money(r.placement_fee_minor)],
              ["Triage officer", r.triage_officer_id ? (mine ? "You" : r.triage_officer_id) : "Unclaimed"],
              ["Confirmed", dateTime(r.confirmed_at)],
              ["Completed", dateTime(r.completed_at)],
            ]}
          />
          {r.client_notes && (
            <div className="mt-4">
              <div className="text-muted-foreground mb-1 text-xs">Client notes</div>
              <TextBlock>{r.client_notes}</TextBlock>
            </div>
          )}
          {r.cancel_reason && (
            <div className="mt-4">
              <div className="text-muted-foreground mb-1 text-xs">Cancelled by {r.cancelled_by}</div>
              <TextBlock>{r.cancel_reason}</TextBlock>
            </div>
          )}
        </Section>
        <Section title="Client and address">
          <DetailList
            className="sm:grid-cols-1"
            items={[
              [
                "Client",
                r.client ? (
                  <Link className="text-primary hover:underline" to={`/clients/${r.client.id}`}>
                    {r.client.full_name}
                  </Link>
                ) : (
                  "—"
                ),
              ],
              ["Phone", r.client?.phone],
              ["ID verification", <StatusBadge value={r.client?.id_verification} />],
              ["Service area", areas.find((a) => a.value === r.address?.service_area_id)?.label ?? "Not in an area"],
              ["Address", [r.address?.house_number, r.address?.street, r.address?.estate].filter(Boolean).join(", ") || "—"],
              ["Landmark", r.address?.landmark],
              ["Provider", r.provider?.display_name ?? "Not yet assigned"],
            ]}
          />
        </Section>
      </div>

      {canShortlist && (
        <Section
          className="mt-4"
          title="Shortlist"
          actions={
            <>
              <Allowed when={can.shortlist && open}>
                <FormDialog
                  trigger={entries.length ? "Regenerate" : "Generate shortlist"}
                  triggerVariant={entries.length ? "outline" : "default"}
                  title="Generate the ranked shortlist"
                  description="Only active, trained and contracted providers with documents in date are considered. Ranked by skill, distance, rating, availability and workload."
                  fields={[{ name: "size", label: "How many providers", type: "number" }]}
                  initial={{ size: 5 }}
                  submitLabel="Generate"
                  onSubmit={(body) => generate.mutateAsync({ ...path, params: { ...path.params, query: { size: body.size } } })}
                />
              </Allowed>
              {selected.length >= 2 && (
                <Button variant="outline" size="sm" onClick={() => setComparing((c) => !c)}>
                  {comparing ? "Hide comparison" : `Compare ${selected.length}`}
                </Button>
              )}
              <Allowed when={can.share && open && selected.length > 0}>
                <FormDialog
                  trigger={`Offer client ${selected.length} choice${selected.length === 1 ? "" : "s"}`}
                  title="Offer the client a choice"
                  description="The client sees these providers' public profiles (never contact details) and picks one."
                  submitLabel="Share"
                  onSubmit={() => share.mutateAsync({ ...path, body: { entry_ids: selected } })}
                  onDone={() => setSelected([])}
                />
              </Allowed>
            </>
          }
        >
          {comparing && compared.length >= 2 && <Comparison entries={compared} />}
          <DataTable<Shortlist>
            rows={entries}
            loading={shortlist.isLoading}
            error={shortlist.error}
            rowKey={(e) => e.id}
            empty="No shortlist yet. Generate one to rank eligible providers."
            rowClassName={(e) => (e.client_selected ? "bg-emerald-50 dark:bg-emerald-950/30" : undefined)}
            columns={[
              {
                header: "",
                cell: (e) => (
                  <Checkbox
                    aria-label="Select"
                    checked={selected.includes(e.id)}
                    onCheckedChange={(c) => setSelected((s) => (c ? [...s, e.id] : s.filter((x) => x !== e.id)))}
                  />
                ),
              },
              { header: "#", cell: (e) => e.rank },
              {
                header: "Provider",
                cell: (e) => (
                  <div>
                    <Link className="text-primary hover:underline" to={`/providers/${e.provider.id}`}>
                      {e.provider.full_name}
                    </Link>
                    <div className="flex gap-1">
                      {e.shown_to_client && <Badge variant="info">Shown to client</Badge>}
                      {e.client_selected && <Badge variant="success">Client's choice</Badge>}
                    </div>
                  </div>
                ),
              },
              { header: "Score", cell: (e) => <span className="font-semibold tabular-nums">{score(e.score, 2)}</span> },
              { header: "Skill", cell: (e) => score(e.score_breakdown?.skill, 2) },
              { header: "Distance", cell: (e) => (e.score_breakdown?.distance_km != null ? `${score(e.score_breakdown.distance_km)} km` : "—") },
              { header: "Rating", cell: (e) => score(e.provider.rating_avg) },
              { header: "Appraisal", cell: (e) => score(e.provider.appraisal_score) },
              { header: "Jobs", cell: (e) => e.provider.completed_jobs ?? 0 },
              {
                header: "",
                cell: (e) => (
                  <Allowed when={can.assign && open}>
                    <FormDialog
                      trigger="Assign"
                      triggerVariant="default"
                      title={`Offer the job to ${e.provider.full_name}`}
                      description="The provider receives the offer by app, SMS and USSD. Only one live offer is allowed per request."
                      fields={[
                        { name: "selection_reason", label: "Why this provider", type: "textarea", required: true },
                        { name: "offer_expires_in_minutes", label: "Offer expires in (minutes)", type: "number" },
                      ]}
                      initial={{ selection_reason: e.rank === 1 ? "Top of the ranked shortlist" : "" }}
                      submitLabel="Send offer"
                      success="Offer sent"
                      onSubmit={(body) =>
                        assign.mutateAsync({ ...path, body: { ...body, provider_id: e.provider.id, shortlist_entry_id: e.id } })
                      }
                    />
                  </Allowed>
                ),
              },
            ]}
          />
        </Section>
      )}

      <Section className="mt-4" title="Offers and assignments">
        <DataTable<Schemas["Assignment"]>
          rows={r.assignments}
          rowKey={(a) => a.id}
          empty="No offers sent yet."
          columns={[
            { header: "Provider", cell: (a) => (a.provider ? <Link className="text-primary hover:underline" to={`/providers/${a.provider.id}`}>{a.provider.full_name}</Link> : "—") },
            { header: "Status", cell: (a) => <StatusBadge value={a.status} /> },
            { header: "Reason", cell: (a) => <span className="line-clamp-2 max-w-xs">{a.selection_reason}</span> },
            { header: "Offered", cell: (a) => dateTime(a.offered_at) },
            { header: "Expires", cell: (a) => dateTime(a.offer_expires_at) },
            { header: "Response", cell: (a) => (a.responded_at ? `${dateTime(a.responded_at)} via ${humanize(a.response_channel)}` : "—") },
            { header: "Decline reason", cell: (a) => a.decline_reason ?? "—" },
            {
              header: "",
              cell: (a) => (
                <Allowed when={can.withdraw && ["offered", "accepted"].includes(a.status)}>
                  <FormDialog
                    trigger={a.status === "offered" ? "Withdraw" : "Replace"}
                    title={a.status === "offered" ? "Withdraw the offer" : "Replace the assigned provider"}
                    fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                    destructive
                    onSubmit={(body) => withdraw.mutateAsync({ params: { path: { id: a.id } }, body })}
                  />
                </Allowed>
              ),
            },
          ]}
        />
      </Section>

      <Section
        className="mt-4"
        title="Visits"
        actions={
          <Allowed when={can.schedule && !["closed", "cancelled", "unfilled"].includes(r.status)}>
            <FormDialog
              trigger="Schedule visit"
              title="Schedule a visit"
              description="For recurring or multi-day work. Add each visit separately."
              fields={[
                { name: "scheduled_start", label: "Start", type: "datetime", required: true },
                { name: "scheduled_end", label: "End", type: "datetime", required: true },
              ]}
              submitLabel="Schedule"
              onSubmit={(body) => schedule.mutateAsync({ ...path, body: { visits: [body] } })}
            />
          </Allowed>
        }
      >
        <DataTable<Schemas["Visit"]>
          rows={r.visits}
          rowKey={(v) => v.id}
          empty="No visits scheduled."
          columns={[
            { header: "Status", cell: (v) => <StatusBadge value={v.status} /> },
            { header: "Scheduled", cell: (v) => `${dateTime(v.scheduled_start)} – ${dateTime(v.scheduled_end)}` },
            { header: "Checked in", cell: (v) => (v.checked_in_at ? `${dateTime(v.checked_in_at)} (${humanize(v.checkin_method)})` : "—") },
            { header: "Checked out", cell: (v) => dateTime(v.checked_out_at) },
            { header: "Client confirmed", cell: (v) => dateTime(v.client_confirmed_at) },
            {
              header: "",
              cell: (v) =>
                v.status === "scheduled" && (
                  <div className="flex justify-end gap-1">
                    <Allowed when={can.checkIn}>
                      <FormDialog
                        trigger="Manual check-in"
                        title="Record check-in for the provider"
                        description="Use when the provider phoned in and could not check in themselves."
                        fields={[
                          { name: "checked_in_at", label: "Checked in at", type: "datetime", required: true },
                          { name: "reason", label: "Why manual", type: "textarea", required: true },
                        ]}
                        initial={{ checked_in_at: new Date().toISOString() }}
                        onSubmit={(body) => manualCheckIn.mutateAsync({ params: { path: { id: v.id } }, body })}
                      />
                    </Allowed>
                    <Allowed when={can.updateVisit}>
                      <FormDialog
                        trigger="Change"
                        title="Reschedule, cancel or mark missed"
                        fields={[
                          { name: "status", label: "Mark as", type: "select", options: ["missed", "cancelled"], help: "Leave empty to reschedule." },
                          { name: "scheduled_start", label: "New start", type: "datetime", show: (s) => !s.status },
                          { name: "scheduled_end", label: "New end", type: "datetime", show: (s) => !s.status },
                          { name: "reason", label: "Reason", type: "textarea", required: true },
                        ]}
                        initial={{ scheduled_start: v.scheduled_start, scheduled_end: v.scheduled_end }}
                        submitLabel="Save"
                        onSubmit={(body) => updateVisit.mutateAsync({ params: { path: { id: v.id } }, body })}
                      />
                    </Allowed>
                  </div>
                ),
            },
          ]}
        />
      </Section>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Section title="Variations">
          {r.variations?.length ? (
            <ul className="divide-y text-sm">
              {r.variations.map((v) => (
                <li key={v.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                  <div>
                    <div>{v.description}</div>
                    <div className="text-muted-foreground text-xs">
                      Raised by {v.raised_by} · client {money(v.client_delta_minor)} · provider {money(v.provider_delta_minor)}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <StatusBadge value={v.status} />
                    {v.status === "pending" && <VariationDecisionButtons id={v.id} />}
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <Empty>No variations.</Empty>
          )}
        </Section>
        <Section title="Exceptions">
          {r.exceptions?.length ? (
            <ul className="divide-y text-sm">
              {r.exceptions.map((x) => (
                <li key={x.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                  <div>
                    <div className="font-medium">{humanize(x.kind)}</div>
                    <div className="text-muted-foreground text-xs">
                      {dateTime(x.detected_at)}
                      {x.resolution ? ` · ${x.resolution}` : ""}
                    </div>
                  </div>
                  {x.reviewed_at ? <StatusBadge value="closed" /> : <ExceptionResolveButton id={x.id} />}
                </li>
              ))}
            </ul>
          ) : (
            <Empty>No exceptions.</Empty>
          )}
        </Section>
      </div>
    </>
  );
}

/** Side-by-side view of the selected shortlist entries, to justify a choice. */
function Comparison({ entries }: { entries: Shortlist[] }) {
  const rows: [string, (e: Shortlist) => string | number][] = [
    ["Rank", (e) => e.rank],
    ["Total score", (e) => score(e.score, 2)],
    ["Skill", (e) => score(e.score_breakdown?.skill, 2)],
    ["Distance (km)", (e) => score(e.score_breakdown?.distance_km)],
    ["Rating factor", (e) => score(e.score_breakdown?.rating, 2)],
    ["Availability", (e) => score(e.score_breakdown?.availability, 2)],
    ["Workload", (e) => score(e.score_breakdown?.workload, 2)],
    ["Average rating", (e) => score(e.provider.rating_avg)],
    ["Appraisal", (e) => score(e.provider.appraisal_score)],
    ["Completed jobs", (e) => e.provider.completed_jobs ?? 0],
  ];
  return (
    <div className="mb-4 overflow-x-auto rounded-lg border">
      <table className="w-full text-sm">
        <thead>
          <tr className="bg-muted/50">
            <th className="p-2 text-left font-medium" />
            {entries.map((e) => (
              <th key={e.id} className="p-2 text-left font-medium">
                {e.provider.full_name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map(([label, fn]) => {
            const values = entries.map(fn);
            return (
              <tr key={label} className="border-t">
                <td className="text-muted-foreground p-2">{label}</td>
                {values.map((v, i) => (
                  <td key={i} className="p-2 tabular-nums">
                    {v}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
