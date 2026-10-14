import { Link, useParams } from "react-router";

import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { $api, type Schemas } from "@/api/client";
import { DisciplinaryKind, EngagementBasis } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useAreaName, useAreaOptions, useCategoryName, useCategoryOptions } from "@/components/lookups";
import { DetailList, Empty, ErrorState, Loading, PageHeader, Section, TextBlock } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { useCan } from "@/lib/access";
import { date, dateTime, humanize, score } from "@/lib/format";

import { DocumentsTable } from "../vetting/documents";
import { DisciplinaryTable } from "./discipline";

export function ProviderPage() {
  const { id = "" } = useParams();
  const path = { params: { path: { id } } };
  const q = $api.useQuery("get", "/console/providers/{id}", path);
  const appraisal = $api.useQuery("get", "/console/providers/{id}/appraisal", path);
  const timeline = $api.useQuery("get", "/console/providers/{id}/timeline", path);
  const discipline = $api.useQuery("get", "/console/providers/{id}/disciplinary-actions", path);
  const canCourses = useCan("GET /console/training-courses");
  const courses = $api.useQuery("get", "/console/training-courses", {}, { enabled: canCourses });

  const update = $api.useMutation("patch", "/console/providers/{id}");
  const activate = $api.useMutation("post", "/console/providers/{id}/activate");
  const suspend = $api.useMutation("post", "/console/providers/{id}/suspend");
  const reinstate = $api.useMutation("post", "/console/providers/{id}/reinstate");
  const discipline_ = $api.useMutation("post", "/console/providers/{id}/disciplinary-actions");
  const reverify = $api.useMutation("post", "/console/providers/{id}/reverification");
  const enrol = $api.useMutation("post", "/console/trainings");

  const can = {
    update: useCan("PATCH /console/providers/{id}"),
    activate: useCan("POST /console/providers/{id}/activate"),
    suspend: useCan("POST /console/providers/{id}/suspend"),
    reinstate: useCan("POST /console/providers/{id}/reinstate"),
    discipline: useCan("POST /console/providers/{id}/disciplinary-actions"),
    reverify: useCan("POST /console/providers/{id}/reverification"),
    enrol: useCan("POST /console/trainings"),
    spotCheck: useCan("POST /console/spot-checks"),
  };
  const categoryName = useCategoryName();
  const categoryOptions = useCategoryOptions();
  const areaName = useAreaName();
  const areaOptions = useAreaOptions();

  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const p = q.data;
  const courseOptions = (courses.data ?? []).filter((c) => c.is_active !== false).map((c) => ({ value: c.id, label: `${c.code} · ${c.name}` }));
  const approvedCategories = (p.categories ?? []).filter((c) => c.status === "approved" || c.status === "active");

  return (
    <>
      <PageHeader
        back={{ to: "/providers", label: "Providers" }}
        title={
          <span className="flex flex-wrap items-center gap-3">
            {p.full_name}
            <StatusBadge value={p.status} />
            {(p.open_complaints ?? 0) > 0 && <Badge variant="warning">{p.open_complaints} open complaints</Badge>}
          </span>
        }
        description={[p.phone, p.activated_at && `active since ${date(p.activated_at)}`].filter(Boolean).join(" · ")}
        actions={
          <>
            <Allowed when={can.activate && ["approved", "active"].includes(p.status)}>
              <FormDialog
                trigger="Activate"
                triggerVariant="default"
                title="Activate for categories"
                description="Needs a passed induction, a signed contract and documents in date. The provider can then be assigned work."
                fields={[
                  {
                    name: "category_ids",
                    label: "Categories",
                    type: "multiselect",
                    required: true,
                    options: approvedCategories.map((c) => ({ value: c.category_id!, label: categoryName(c.category_id) })),
                  },
                ]}
                initial={{ category_ids: approvedCategories.filter((c) => c.status === "approved").map((c) => c.category_id) }}
                onSubmit={(body) => activate.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.update}>
              <FormDialog
                trigger="Edit settings"
                title="Provider settings"
                fields={[
                  { name: "engagement_basis", label: "Engagement basis", type: "select", options: EngagementBasis },
                  { name: "max_concurrent_jobs", label: "Max concurrent jobs", type: "number" },
                  { name: "service_area_ids", label: "Service areas", type: "multiselect", options: areaOptions },
                  { name: "bio", label: "Public bio", type: "textarea" },
                ]}
                initial={p as unknown as Record<string, unknown>}
                submitLabel="Save"
                onSubmit={(body) => update.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.enrol}>
              <FormDialog
                trigger="Enrol in training"
                title="Enrol in a course"
                fields={[
                  { name: "course_id", label: "Course", type: "select", options: courseOptions, required: true },
                  { name: "scheduled_for", label: "Scheduled for", type: "datetime" },
                  { name: "is_retraining", label: "This is retraining", type: "checkbox" },
                ]}
                onSubmit={(body) => enrol.mutateAsync({ body: { ...body, provider_id: p.id } })}
              />
            </Allowed>
            <Allowed when={can.reverify}>
              <FormDialog
                trigger="Re-verify"
                title="Open a re-verification"
                fields={[{ name: "category_id", label: "Category", type: "select", options: categoryOptions, required: true }]}
                onSubmit={(body) => reverify.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.discipline}>
              <FormDialog
                trigger="Disciplinary action"
                title="Apply a disciplinary action"
                description="Disqualification needs a second officer to authorise it before it takes effect."
                fields={[
                  { name: "kind", label: "Action", type: "select", options: DisciplinaryKind, required: true },
                  { name: "reason", label: "Reason", type: "textarea", required: true },
                  { name: "complaint_id", label: "Related complaint ID" },
                  { name: "effective_to", label: "Until", type: "datetime", show: (s) => s.kind === "suspension" },
                  { name: "retraining_course_id", label: "Retraining course", type: "select", options: courseOptions, show: (s) => s.kind === "retraining" },
                ]}
                onSubmit={(body) => discipline_.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.suspend && p.status === "active"}>
              <FormDialog
                trigger="Suspend"
                triggerVariant="destructive"
                title="Suspend provider"
                description="They stop receiving offers straight away. Open assignments need replacing."
                fields={[
                  { name: "reason", label: "Reason", type: "textarea", required: true },
                  { name: "until", label: "Until", type: "datetime" },
                ]}
                destructive
                onSubmit={(body) => suspend.mutateAsync({ ...path, body })}
              />
            </Allowed>
            <Allowed when={can.reinstate && p.status === "suspended"}>
              <FormDialog
                trigger="Reinstate"
                title="Lift the suspension"
                description="If retraining was required, it must be passed first."
                fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                onSubmit={(body) => reinstate.mutateAsync({ ...path, body })}
              />
            </Allowed>
          </>
        }
      />

      {p.suspended_reason && p.status === "suspended" && (
        <div className="border-destructive/30 bg-destructive/5 mb-4 rounded-lg border p-3 text-sm">
          <span className="font-medium">Suspended:</span> {p.suspended_reason}
        </div>
      )}

      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="documents">Documents ({p.documents?.length ?? 0})</TabsTrigger>
          <TabsTrigger value="applications">Applications</TabsTrigger>
          <TabsTrigger value="training">Training</TabsTrigger>
          <TabsTrigger value="discipline">Discipline</TabsTrigger>
          <TabsTrigger value="appraisal">Appraisal</TabsTrigger>
          <TabsTrigger value="timeline">Timeline</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="grid gap-4 lg:grid-cols-3">
          <Section title="Profile" className="lg:col-span-2">
            <DetailList
              items={[
                ["Engagement basis", humanize(p.engagement_basis)],
                ["Date of birth", date(p.date_of_birth)],
                ["Home area", areaName(p.home_service_area_id)],
                ["Service areas", (p.service_area_ids ?? []).map(areaName).join(", ") || "—"],
                ["Years of experience", p.years_experience ?? "—"],
                ["Languages", (p.languages ?? []).join(", ") || "—"],
                ["Payout number", p.payout_msisdn],
                ["Max concurrent jobs", p.max_concurrent_jobs ?? "—"],
                ["Rating", `${score(p.rating_avg)} (${p.rating_count ?? 0} ratings)`],
                ["Appraisal score", score(p.appraisal_score)],
                ["Completed jobs", p.completed_jobs ?? 0],
                ["Open complaints", p.open_complaints ?? 0],
              ]}
            />
            {p.bio && (
              <div className="mt-4">
                <div className="text-muted-foreground mb-1 text-xs">Bio</div>
                <TextBlock>{p.bio}</TextBlock>
              </div>
            )}
          </Section>
          <div className="grid content-start gap-4">
            <Section title="Categories">
              {p.categories?.length ? (
                <ul className="space-y-2 text-sm">
                  {p.categories.map((c) => (
                    <li key={c.category_id} className="flex items-center justify-between">
                      {categoryName(c.category_id)}
                      <StatusBadge value={c.status} />
                    </li>
                  ))}
                </ul>
              ) : (
                <Empty>None yet.</Empty>
              )}
            </Section>
            <Section title="Contract">
              {p.contract ? (
                <DetailList
                  className="sm:grid-cols-1"
                  items={[
                    ["Contract version", p.contract.contract_version],
                    ["Code of conduct", p.contract.code_of_conduct_version],
                    ["Signed", dateTime(p.contract.signed_at)],
                  ]}
                />
              ) : (
                <Empty>Not signed yet.</Empty>
              )}
            </Section>
          </div>
        </TabsContent>

        <TabsContent value="documents">
          <Section title="Documents">
            <DocumentsTable docs={p.documents} purpose={`Provider file review: ${p.full_name}`} />
          </Section>
        </TabsContent>

        <TabsContent value="applications">
          <Section title="Applications">
            <DataTable<Schemas["ApplicationSummary"]>
              rows={p.applications}
              rowKey={(a) => a.id}
              rowHref={(a) => `/applications/${a.id}`}
              empty="No applications."
              columns={[
                { header: "Reference", cell: (a) => <span className="font-mono text-xs">{a.reference}</span> },
                { header: "Category", cell: (a) => categoryName(a.category_id) },
                { header: "Status", cell: (a) => <StatusBadge value={a.status} /> },
                { header: "Submitted", cell: (a) => dateTime(a.submitted_at) },
                { header: "", cell: (a) => (a.is_reverification ? <Badge variant="info">Re-verification</Badge> : null) },
              ]}
            />
          </Section>
        </TabsContent>

        <TabsContent value="training">
          <Section title="Training">
            <DataTable<Schemas["TrainingEnrolment"]>
              rows={p.trainings}
              rowKey={(t) => t.id}
              empty="No training yet."
              columns={[
                { header: "Course", cell: (t) => `${t.course.code} · ${t.course.name}` },
                { header: "Status", cell: (t) => <StatusBadge value={t.status} /> },
                { header: "Scheduled", cell: (t) => dateTime(t.scheduled_for) },
                { header: "Attended", cell: (t) => dateTime(t.attended_at) },
                { header: "Score", cell: (t) => score(t.score, 0) },
                { header: "", cell: (t) => (t.is_retraining ? <Badge variant="warning">Retraining</Badge> : null) },
              ]}
            />
          </Section>
        </TabsContent>

        <TabsContent value="discipline">
          <Section title="Disciplinary history">
            <DisciplinaryTable actions={discipline.data ?? p.disciplinary_actions} />
          </Section>
        </TabsContent>

        <TabsContent value="appraisal">
          <Section title="Appraisal">
            {appraisal.error ? (
              <ErrorState error={appraisal.error} />
            ) : appraisal.data ? (
              <>
                <DetailList
                  items={[
                    ["Total score", <span className="text-lg font-semibold">{score(appraisal.data.total_score)}</span>],
                    ["Period", `${date(appraisal.data.period_start)} – ${date(appraisal.data.period_end)}`],
                    ["Ratings", score(appraisal.data.components?.rating)],
                    ["Complaints", score(appraisal.data.components?.complaints)],
                    ["Spot checks", score(appraisal.data.components?.spot_checks)],
                    ["Reliability", score(appraisal.data.components?.reliability)],
                    ["Weights", Object.entries(appraisal.data.weights ?? {}).map(([k, v]) => `${k} ${String(v)}`).join(", ")],
                  ]}
                />
                {appraisal.data.history?.length ? (
                  <div className="mt-4 flex h-32 items-end gap-1">
                    {appraisal.data.history.map((h, i) => (
                      <div key={i} className="flex flex-1 flex-col items-center gap-1" title={`${date(h.period_end)}: ${score(h.total_score)}`}>
                        <div className="bg-primary/70 w-full rounded-t" style={{ height: `${Math.max(4, Math.min(100, h.total_score ?? 0))}%` }} />
                        <span className="text-muted-foreground text-[10px]">{date(h.period_end).replace(/ \d{4}$/, "")}</span>
                      </div>
                    ))}
                  </div>
                ) : null}
              </>
            ) : (
              <Loading />
            )}
          </Section>
        </TabsContent>

        <TabsContent value="timeline">
          <Section title="Timeline">
            <Timeline entries={timeline.data?.data} loading={timeline.isLoading} error={timeline.error} />
          </Section>
        </TabsContent>
      </Tabs>
      <Allowed when={can.spotCheck}>
        <p className="text-muted-foreground mt-4 text-xs">
          Record a spot check for this provider from <Link className="text-primary hover:underline" to={`/spot-checks?provider_id=${p.id}`}>Spot checks</Link>.
        </p>
      </Allowed>
    </>
  );
}

export function Timeline({ entries, loading, error }: { entries: Schemas["AuditEntry"][] | undefined; loading?: boolean; error?: unknown }) {
  return (
    <DataTable<Schemas["AuditEntry"]>
      rows={entries}
      loading={loading}
      error={error}
      rowKey={(e) => e.id}
      empty="Nothing recorded yet."
      columns={[
        { header: "When", cell: (e) => dateTime(e.occurred_at) },
        { header: "Action", cell: (e) => <span className="font-mono text-xs">{e.action}</span> },
        { header: "Entity", cell: (e) => humanize(e.entity_type) },
        { header: "By", cell: (e) => (e.actor_kind ? humanize(e.actor_kind) : "System") },
        { header: "Reason", cell: (e) => <span className="line-clamp-2 max-w-sm text-xs">{e.reason}</span> },
      ]}
    />
  );
}
