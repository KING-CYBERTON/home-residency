import { Link, useParams } from "react-router";

import { Badge } from "@/components/ui/badge";
import { $api, type Schemas } from "@/api/client";
import { ClearanceResult, RefereeOutcome } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { DocumentButton } from "@/components/document-button";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useCategoryName } from "@/components/lookups";
import { DetailList, Empty, ErrorState, Loading, PageHeader, Section, TextBlock } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { useCan, useMe } from "@/lib/access";
import { date, dateTime, humanize } from "@/lib/format";

import { Progress } from "./applications";

export function ApplicationPage() {
  const { id = "" } = useParams();
  const me = useMe();
  const categoryName = useCategoryName();
  const path = { params: { path: { id } } };
  const q = $api.useQuery("get", "/console/applications/{id}", path);

  const claim = $api.useMutation("post", "/console/applications/{id}/claim");
  const ret = $api.useMutation("post", "/console/applications/{id}/return");
  const review = $api.useMutation("patch", "/console/applications/{id}/items/{itemId}");
  const recordReferee = $api.useMutation("post", "/console/applications/{id}/referees/{refereeId}/record");
  const requestClearance = $api.useMutation("post", "/console/applications/{id}/clearance-checks");
  const updateClearance = $api.useMutation("patch", "/console/clearance-checks/{id}");
  const assess = $api.useMutation("post", "/console/applications/{id}/assessments");
  const complete = $api.useMutation("post", "/console/applications/{id}/complete-vetting");
  const decide = $api.useMutation("post", "/console/applications/{id}/decision");
  const verifyDoc = $api.useMutation("post", "/console/documents/{id}/verify");
  const rejectDoc = $api.useMutation("post", "/console/documents/{id}/reject");

  const can = {
    claim: useCan("POST /console/applications/{id}/claim"),
    ret: useCan("POST /console/applications/{id}/return"),
    review: useCan("PATCH /console/applications/{id}/items/{itemId}"),
    referee: useCan("POST /console/applications/{id}/referees/{refereeId}/record"),
    clearance: useCan("POST /console/applications/{id}/clearance-checks"),
    clearanceUpdate: useCan("PATCH /console/clearance-checks/{id}"),
    assess: useCan("POST /console/applications/{id}/assessments"),
    complete: useCan("POST /console/applications/{id}/complete-vetting"),
    decide: useCan("POST /console/applications/{id}/decision"),
    verifyDoc: useCan("POST /console/documents/{id}/verify"),
    rejectDoc: useCan("POST /console/documents/{id}/reject"),
  };

  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const a = q.data;
  const vetting = a.status === "in_vetting";
  const mine = a.vetting_officer_id === me?.id;
  const items = a.items ?? [];
  const docItems = items.filter((i) => i.document_id);
  const purpose = `Vetting application ${a.reference}`;
  // The database refuses an approval by the officer who vetted the application.
  const sameOfficer = a.vetting_officer_id === me?.id;

  return (
    <>
      <PageHeader
        back={{ to: "/applications", label: "Applications" }}
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{a.reference}</span>
            <StatusBadge value={a.status} />
            {a.is_reverification && <Badge variant="info">Re-verification</Badge>}
          </span>
        }
        description={
          <>
            {a.provider_id ? (
              <Link className="text-primary hover:underline" to={`/providers/${a.provider_id}`}>
                {a.provider_name ?? "Provider"}
              </Link>
            ) : (
              a.provider_name
            )}{" "}
            · {categoryName(a.category_id)} · submitted {dateTime(a.submitted_at)}
          </>
        }
        actions={
          <>
            <Allowed when={can.claim && ["submitted", "incomplete", "in_vetting"].includes(a.status) && !mine}>
              <FormDialog
                trigger={a.vetting_officer_id ? "Take over" : "Claim"}
                triggerVariant="default"
                title="Claim this application"
                description="You become its vetting officer. A different officer must approve it."
                submitLabel="Claim"
                onSubmit={() => claim.mutateAsync(path)}
              />
            </Allowed>
            <Allowed when={can.ret && vetting}>
              <FormDialog
                trigger="Return as incomplete"
                title="Return to the applicant"
                description="The applicant is told what is missing and can resubmit."
                fields={[
                  { name: "message", label: "What is missing", type: "textarea", required: true },
                  {
                    name: "item_ids",
                    label: "Checklist items to redo",
                    type: "multiselect",
                    options: items.map((i) => ({ value: i.id, label: i.template_item.label })),
                  },
                ]}
                onSubmit={(body) => ret.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.complete && vetting}>
              <FormDialog
                trigger="Complete vetting"
                triggerVariant="default"
                title="Finish vetting and send for approval"
                description="Every mandatory checklist item must be satisfied or waived."
                fields={[{ name: "vetting_report", label: "Vetting report", type: "textarea", required: true }]}
                initial={{ vetting_report: a.vetting_report ?? "" }}
                onSubmit={(body) => complete.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.decide && a.status === "awaiting_approval"}>
              <FormDialog
                trigger="Decide"
                triggerVariant="default"
                title="Approve, defer or reject"
                description={
                  sameOfficer
                    ? "You vetted this application, so a different approver must decide it."
                    : "Approval lets the provider move on to training and contract signing."
                }
                fields={[
                  { name: "decision", label: "Decision", type: "select", options: ["approve", "defer", "reject"], required: true },
                  { name: "reason", label: "Reason", type: "textarea", required: true },
                ]}
                disabled={sameOfficer}
                submitLabel="Record decision"
                onSubmit={(body) => decide.mutateAsync({ ...path, body })}
              />
            </Allowed>
          </>
        }
      />

      {a.missing && a.missing.length > 0 && (
        <div className="mb-4 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-200">
          <div className="font-medium">Still missing</div>
          <ul className="mt-1 list-disc pl-5">
            {a.missing.map((m) => (
              <li key={m}>{m}</li>
            ))}
          </ul>
        </div>
      )}

      <Section
        title="Checklist"
        actions={a.checklist_progress && <Progress done={a.checklist_progress.satisfied ?? 0} total={a.checklist_progress.total ?? 0} />}
      >
        <DataTable<Schemas["ApplicationItem"]>
          rows={items}
          rowKey={(i) => i.id}
          empty="This application has no checklist items."
          columns={[
            {
              header: "Item",
              cell: (i) => (
                <div>
                  <div>{i.template_item.label}</div>
                  <div className="text-muted-foreground text-xs">
                    {humanize(i.template_item.kind)}
                    {i.template_item.document_type ? ` · ${humanize(i.template_item.document_type)}` : ""}
                    {i.template_item.is_mandatory ? " · mandatory" : ""}
                  </div>
                </div>
              ),
            },
            { header: "Status", cell: (i) => <StatusBadge value={i.status} /> },
            { header: "Notes", cell: (i) => <span className="line-clamp-2 max-w-xs text-xs">{i.notes}</span> },
            { header: "Reviewed", cell: (i) => dateTime(i.reviewed_at) },
            {
              header: "",
              cell: (i) => (
                <div className="flex flex-wrap justify-end gap-1">
                  {i.document_id && <DocumentButton id={i.document_id} purpose={purpose} />}
                  <Allowed when={!!i.document_id && can.verifyDoc && vetting}>
                    <FormDialog
                      trigger="Verify doc"
                      title="Verify document"
                      fields={[
                        { name: "issued_on", label: "Issued on", type: "date" },
                        { name: "expires_on", label: "Expires on", type: "date" },
                        { name: "notes", label: "Notes", type: "textarea" },
                      ]}
                      onSubmit={(body) => verifyDoc.mutateAsync({ params: { path: { id: i.document_id! } }, body })}
                    />
                  </Allowed>
                  <Allowed when={!!i.document_id && can.rejectDoc && vetting}>
                    <FormDialog
                      trigger="Reject doc"
                      title="Reject document"
                      fields={[{ name: "reason", label: "Reason (shown to the provider)", type: "textarea", required: true }]}
                      destructive
                      onSubmit={(body) => rejectDoc.mutateAsync({ params: { path: { id: i.document_id! } }, body })}
                    />
                  </Allowed>
                  <Allowed when={can.review && vetting}>
                    <FormDialog
                      trigger="Review"
                      triggerVariant="default"
                      title={`Review: ${i.template_item.label}`}
                      fields={[
                        { name: "status", label: "Result", type: "select", options: ["satisfied", "failed", "waived", "pending"], required: true },
                        { name: "notes", label: "Notes", type: "textarea" },
                      ]}
                      initial={{ status: i.status === "submitted" ? "satisfied" : i.status, notes: i.notes }}
                      submitLabel="Save"
                      onSubmit={(body) => review.mutateAsync({ params: { path: { id, itemId: i.id } }, body })}
                    />
                  </Allowed>
                </div>
              ),
            },
          ]}
        />
      </Section>

      <Section className="mt-4" title="Referees">
        <DataTable<Schemas["Referee"]>
          rows={a.referees}
          rowKey={(r) => r.id}
          empty="No referees given."
          columns={[
            {
              header: "Referee",
              cell: (r) => (
                <div>
                  <div>{r.full_name}</div>
                  <div className="text-muted-foreground text-xs">{[r.relationship, r.phone].filter(Boolean).join(" · ")}</div>
                </div>
              ),
            },
            {
              header: "Employment",
              cell: (r) => (r.employer_name ? `${r.employer_name} (${date(r.employment_from)} – ${date(r.employment_to)})` : "—"),
            },
            { header: "Outcome", cell: (r) => <StatusBadge value={r.outcome} /> },
            { header: "Statement", cell: (r) => <span className="line-clamp-3 max-w-sm text-xs">{r.statement}</span> },
            { header: "Contacted", cell: (r) => dateTime(r.contacted_at) },
            {
              header: "",
              cell: (r) => (
                <Allowed when={can.referee && vetting}>
                  <FormDialog
                    trigger="Record call"
                    title={`Record conversation with ${r.full_name}`}
                    fields={[
                      { name: "outcome", label: "Outcome", type: "select", options: RefereeOutcome, required: true },
                      { name: "statement", label: "What they said", type: "textarea", required: true },
                      { name: "contacted_at", label: "Contacted at", type: "datetime" },
                    ]}
                    initial={{ outcome: r.outcome, statement: r.statement, contacted_at: new Date().toISOString() }}
                    onSubmit={(body) => recordReferee.mutateAsync({ params: { path: { id, refereeId: r.id } }, body })}
                  />
                </Allowed>
              ),
            },
          ]}
        />
      </Section>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Section
          title="Clearance checks"
          actions={
            <Allowed when={can.clearance && vetting && docItems.length > 0}>
              <FormDialog
                trigger="Request check"
                title="Request independent verification"
                description="For example a Certificate of Good Conduct checked with the DCI."
                fields={[
                  {
                    name: "document_id",
                    label: "Document",
                    type: "select",
                    required: true,
                    options: docItems.map((i) => ({ value: i.document_id!, label: i.template_item.label })),
                  },
                  { name: "agency_name", label: "Agency" },
                  { name: "agency_reference", label: "Agency reference" },
                ]}
                onSubmit={(body) => requestClearance.mutateAsync({ ...path, body })}
              />
            </Allowed>
          }
        >
          {a.clearance_checks?.length ? (
            <ul className="divide-y text-sm">
              {a.clearance_checks.map((c) => (
                <li key={c.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                  <div>
                    <div>
                      {c.agency_name ?? "Agency"} {c.agency_reference && <span className="font-mono text-xs">#{c.agency_reference}</span>}
                    </div>
                    <div className="text-muted-foreground text-xs">
                      Requested {dateTime(c.requested_at)}
                      {c.confirmed_at && ` · confirmed ${dateTime(c.confirmed_at)}`}
                      {c.notes && ` · ${c.notes}`}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <StatusBadge value={c.result} />
                    <Allowed when={can.clearanceUpdate && c.result === "pending"}>
                      <FormDialog
                        trigger="Record result"
                        title="Record clearance result"
                        fields={[
                          { name: "result", label: "Result", type: "select", options: ClearanceResult.filter((r) => r !== "pending"), required: true },
                          { name: "agency_reference", label: "Agency reference" },
                          { name: "notes", label: "Notes", type: "textarea" },
                        ]}
                        initial={{ agency_reference: c.agency_reference }}
                        onSubmit={(body) => updateClearance.mutateAsync({ params: { path: { id: c.id } }, body })}
                      />
                    </Allowed>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <Empty>No clearance checks yet.</Empty>
          )}
        </Section>

        <Section
          title="Interviews and assessments"
          actions={
            <Allowed when={can.assess && vetting}>
              <FormDialog
                trigger="Record assessment"
                title="Record an interview or practical assessment"
                fields={[
                  { name: "kind", label: "Kind", type: "select", options: ["interview", "practical"], required: true },
                  { name: "rubric_version", label: "Rubric version", required: true },
                  { name: "scores", label: "Scores by criterion (JSON)", type: "json", required: true, help: 'For example {"hygiene": 4, "communication": 5}' },
                  { name: "total_score", label: "Total score", type: "number", required: true },
                  { name: "passed", label: "Passed", type: "checkbox" },
                  { name: "notes", label: "Notes", type: "textarea" },
                ]}
                initial={{ kind: "interview", rubric_version: "v1", scores: {}, passed: false }}
                onSubmit={(body) => assess.mutateAsync({ ...path, body })}
              />
            </Allowed>
          }
        >
          {a.assessments?.length ? (
            <ul className="divide-y text-sm">
              {a.assessments.map((s, i) => (
                <li key={s.id ?? i} className="py-2">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">{humanize(s.kind)}</span>
                    <span className="flex items-center gap-2">
                      <span className="tabular-nums">{s.total_score}</span>
                      <StatusBadge value={s.passed ? "passed" : "failed"} />
                    </span>
                  </div>
                  <div className="text-muted-foreground text-xs">
                    Rubric {s.rubric_version} · {dateTime(s.assessed_at)} · {Object.entries(s.scores ?? {}).map(([k, v]) => `${k} ${String(v)}`).join(", ")}
                  </div>
                  {s.notes && <div className="mt-1 text-xs">{s.notes}</div>}
                </li>
              ))}
            </ul>
          ) : (
            <Empty>No assessments yet.</Empty>
          )}
        </Section>
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Section title="Application answers">
          {a.answers && Object.keys(a.answers).length ? (
            <DetailList items={Object.entries(a.answers).map(([k, v]) => [humanize(k), typeof v === "object" ? JSON.stringify(v) : String(v)])} />
          ) : (
            <Empty>No answers recorded.</Empty>
          )}
        </Section>
        <Section title="Report and decision">
          <div className="text-muted-foreground mb-1 text-xs">Vetting report</div>
          <TextBlock>{a.vetting_report}</TextBlock>
          {a.decided_at && (
            <div className="mt-4">
              <div className="text-muted-foreground mb-1 text-xs">Decision on {dateTime(a.decided_at)}</div>
              <TextBlock>{a.decision_reason}</TextBlock>
            </div>
          )}
        </Section>
      </div>
    </>
  );
}
