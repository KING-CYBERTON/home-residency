import { Link, useParams } from "react-router";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { $api } from "@/api/client";
import { DisciplinaryKind, NotificationChannel } from "@/api/enums.gen";
import { DocumentButton } from "@/components/document-button";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { DetailList, Empty, ErrorState, Loading, PageHeader, Section, TextBlock } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { useCan, useMe } from "@/lib/access";
import { dateTime, humanize } from "@/lib/format";

import { DisciplinaryTable } from "../providers/discipline";

export function ComplaintPage() {
  const { id = "" } = useParams();
  const me = useMe();
  const path = { params: { path: { id } } };
  const q = $api.useQuery("get", "/console/complaints/{id}", path);
  const canCourses = useCan("GET /console/training-courses");
  const courses = $api.useQuery("get", "/console/training-courses", {}, { enabled: canCourses });

  const assign = $api.useMutation("post", "/console/complaints/{id}/assign");
  const addEvent = $api.useMutation("post", "/console/complaints/{id}/events");
  const requestAccount = $api.useMutation("post", "/console/complaints/{id}/request-provider-account");
  const recordAccount = $api.useMutation("post", "/console/complaints/{id}/provider-account");
  const report = $api.useMutation("post", "/console/complaints/{id}/report");
  const decide = $api.useMutation("post", "/console/complaints/{id}/decision");
  const communicate = $api.useMutation("post", "/console/complaints/{id}/communicate");
  const close = $api.useMutation("post", "/console/complaints/{id}/close");

  const can = {
    assign: useCan("POST /console/complaints/{id}/assign"),
    event: useCan("POST /console/complaints/{id}/events"),
    requestAccount: useCan("POST /console/complaints/{id}/request-provider-account"),
    recordAccount: useCan("POST /console/complaints/{id}/provider-account"),
    report: useCan("POST /console/complaints/{id}/report"),
    decide: useCan("POST /console/complaints/{id}/decision"),
    communicate: useCan("POST /console/complaints/{id}/communicate"),
    close: useCan("POST /console/complaints/{id}/close"),
    refund: useCan("POST /console/refunds"),
  };

  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const c = q.data;
  const active = !["closed", "withdrawn"].includes(c.status);
  const courseOptions = (courses.data ?? []).map((x) => ({ value: x.id, label: `${x.code} · ${x.name}` }));
  // The database refuses a decision by the investigator, and one made before the provider was heard.
  const isInvestigator = c.investigator_id === me?.id;

  return (
    <>
      <PageHeader
        back={{ to: "/complaints", label: "Complaints" }}
        title={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{c.reference}</span>
            <StatusBadge value={c.severity} />
            <StatusBadge value={c.status} />
            {c.interim_suspension && <Badge variant="destructive">Provider suspended pending outcome</Badge>}
          </span>
        }
        description={`${c.summary} · opened ${dateTime(c.created_at)} from ${humanize(c.source)}`}
        actions={
          <>
            <Allowed when={can.assign && active}>
              <FormDialog
                trigger={c.investigator_id ? "Reassign" : "Assign investigator"}
                triggerVariant={c.investigator_id ? "outline" : "default"}
                title="Assign an investigator"
                description="Usually a supervisor. The investigator cannot also decide the outcome."
                fields={[{ name: "investigator_id", label: "Investigator's staff ID", required: true }]}
                initial={{ investigator_id: c.investigator_id ?? "" }}
                onSubmit={(body) => assign.mutateAsync({ ...path, body })}
              >
                {me && (
                  <p className="text-muted-foreground text-xs">
                    Your staff ID is <code className="select-all">{me.id}</code>
                  </p>
                )}
              </FormDialog>
            </Allowed>
            <Allowed when={can.requestAccount && active && !c.provider_heard_at && !!c.provider}>
              <FormDialog
                trigger="Ask provider for account"
                title="Ask the provider for their account"
                description="The provider is notified and can respond in their portal."
                submitLabel="Send request"
                onSubmit={() => requestAccount.mutateAsync(path)}
              />
            </Allowed>
            <Allowed when={can.recordAccount && active && !c.provider_heard_at && !!c.provider}>
              <FormDialog
                trigger="Record provider's account"
                title="Record the provider's account"
                description="For an account taken by phone or in person."
                fields={[{ name: "account", label: "What the provider said", type: "textarea", required: true }]}
                onSubmit={(body) => recordAccount.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.report && ["investigating", "awaiting_provider_account"].includes(c.status)}>
              <FormDialog
                trigger="Submit report"
                triggerVariant="default"
                title="Submit the investigation report"
                fields={[{ name: "investigation_report", label: "Findings", type: "textarea", required: true }]}
                initial={{ investigation_report: c.investigation_report ?? "" }}
                onSubmit={(body) => report.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.decide && c.status === "awaiting_decision"}>
              <FormDialog
                trigger="Decide"
                triggerVariant="default"
                title="Decide the outcome"
                description={
                  isInvestigator
                    ? "You investigated this complaint, so someone else must decide it."
                    : "Choose a step on the disciplinary ladder. A disqualification then needs a second officer."
                }
                disabled={isInvestigator}
                fields={[
                  { name: "action", label: "Action", type: "select", options: DisciplinaryKind, required: true },
                  { name: "outcome_summary", label: "Outcome (shared with the client)", type: "textarea", required: true },
                  { name: "reason", label: "Reason (internal)", type: "textarea", required: true },
                  { name: "suspension_until", label: "Suspended until", type: "datetime", show: (s) => s.action === "suspension" },
                  { name: "retraining_course_id", label: "Retraining course", type: "select", options: courseOptions, show: (s) => s.action === "retraining" },
                ]}
                onSubmit={(body) => decide.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.communicate && ["decided", "closed"].includes(c.status)}>
              <FormDialog
                trigger="Communicate outcome"
                title="Tell the client or provider what was done"
                fields={[
                  { name: "to", label: "To", type: "select", options: ["client", "provider"], required: true },
                  { name: "message", label: "Message", type: "textarea", required: true },
                  { name: "channels", label: "Channels", type: "multiselect", options: NotificationChannel },
                ]}
                initial={{ to: "client", message: c.outcome_summary ?? "", channels: ["sms", "in_app"] }}
                onSubmit={(body) => communicate.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.close && c.status === "decided"}>
              <FormDialog
                trigger="Close case"
                title="Close the case"
                description="Both parties should have been told the outcome."
                submitLabel="Close"
                onSubmit={() => close.mutateAsync(path)}
              />
            </Allowed>
            <Allowed when={can.refund}>
              <Button variant="outline" size="sm" asChild>
                <Link to={`/refunds?new=1&complaint_id=${c.id}`}>Request refund</Link>
              </Button>
            </Allowed>
          </>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <Section title="Case" className="lg:col-span-2">
          <DetailList
            items={[
              ["Client", c.client ? <Link className="text-primary hover:underline" to={`/clients/${c.client.id}`}>{c.client.full_name}</Link> : "—"],
              ["Provider", c.provider ? <Link className="text-primary hover:underline" to={`/providers/${c.provider.id}`}>{c.provider.full_name}</Link> : "—"],
              ["Request", c.request_id ? <Link className="text-primary hover:underline" to={`/requests/${c.request_id}`}>Open request</Link> : "—"],
              ["Investigator", c.investigator_id ? (isInvestigator ? "You" : <code className="text-xs">{c.investigator_id}</code>) : "Not assigned"],
              ["Provider heard", dateTime(c.provider_heard_at)],
              ["Decided", dateTime(c.decided_at)],
              ["Client informed", dateTime(c.client_informed_at)],
              ["Provider informed", dateTime(c.provider_informed_at)],
            ]}
          />
          <div className="mt-4 grid gap-4">
            <div>
              <div className="text-muted-foreground mb-1 text-xs">Details</div>
              <TextBlock>{c.details}</TextBlock>
            </div>
            {c.provider_account && (
              <div>
                <div className="text-muted-foreground mb-1 text-xs">Provider's account</div>
                <TextBlock>{c.provider_account}</TextBlock>
              </div>
            )}
            {c.investigation_report && (
              <div>
                <div className="text-muted-foreground mb-1 text-xs">Investigation report</div>
                <TextBlock>{c.investigation_report}</TextBlock>
              </div>
            )}
            {c.outcome_summary && (
              <div>
                <div className="text-muted-foreground mb-1 text-xs">Outcome</div>
                <TextBlock>{c.outcome_summary}</TextBlock>
              </div>
            )}
          </div>
        </Section>
        <Section
          title="Case notes"
          actions={
            <Allowed when={can.event && active}>
              <FormDialog
                trigger="Add note"
                title="Add a note or evidence"
                fields={[
                  { name: "event_type", label: "Type", type: "select", options: ["note", "evidence"], required: true },
                  { name: "body", label: "Note", type: "textarea" },
                  { name: "document_id", label: "Evidence document ID", show: (s) => s.event_type === "evidence" },
                  { name: "is_internal", label: "Internal only (not shown to client or provider)", type: "checkbox" },
                ]}
                initial={{ event_type: "note", is_internal: true }}
                onSubmit={(body) => addEvent.mutateAsync({ ...path, body })}
              />
            </Allowed>
          }
        >
          {c.events?.length ? (
            <ol className="space-y-3 text-sm">
              {[...c.events].reverse().map((e) => (
                <li key={e.id} className="border-l-2 pl-3">
                  <div className="text-muted-foreground flex flex-wrap items-center gap-2 text-xs">
                    {dateTime(e.created_at)} · {humanize(e.event_type)}
                    {e.is_internal && <Badge variant="secondary">Internal</Badge>}
                  </div>
                  {e.body && <div className="mt-1 whitespace-pre-wrap">{e.body}</div>}
                  {e.document_id && <DocumentButton id={e.document_id} purpose={`Complaint ${c.reference} evidence`} label="Evidence" />}
                </li>
              ))}
            </ol>
          ) : (
            <Empty>No notes yet.</Empty>
          )}
        </Section>
      </div>

      <Section className="mt-4" title="Disciplinary actions">
        <DisciplinaryTable actions={c.actions} />
      </Section>
    </>
  );
}
