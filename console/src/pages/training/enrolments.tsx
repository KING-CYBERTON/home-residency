import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { TrainingStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan } from "@/lib/access";
import { dateTime, score } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function TrainingsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status", "course_id", "provider_id", "from", "to"], pager.reset);
  const courses = $api.useQuery("get", "/console/training-courses", {});
  const enrol = $api.useMutation("post", "/console/trainings");
  const result = $api.useMutation("patch", "/console/trainings/{id}");
  const can = { enrol: useCan("POST /console/trainings"), result: useCan("PATCH /console/trainings/{id}") };
  const courseOptions = (courses.data ?? []).map((c) => ({ value: c.id, label: `${c.code} · ${c.name}` }));
  const { data, isLoading, error } = $api.useQuery("get", "/console/trainings", {
    params: {
      query: {
        status: f.status as Schemas["TrainingStatus"] | undefined,
        course_id: f.course_id,
        provider_id: f.provider_id,
        from: f.from,
        to: f.to,
        cursor: pager.cursor,
      },
    },
  });
  return (
    <>
      <PageHeader
        title="Training"
        description="Induction and retraining sessions. A provider needs a passed induction before activation."
        actions={
          <Allowed when={can.enrol}>
            <FormDialog
              trigger="Enrol provider"
              triggerVariant="default"
              title="Enrol a provider"
              fields={[
                { name: "provider_id", label: "Provider ID", required: true, help: "Copy it from the provider's file." },
                { name: "course_id", label: "Course", type: "select", options: courseOptions, required: true },
                { name: "scheduled_for", label: "Scheduled for", type: "datetime" },
                { name: "is_retraining", label: "This is retraining", type: "checkbox" },
                { name: "complaint_id", label: "Related complaint ID", show: (s) => Boolean(s.is_retraining) },
              ]}
              initial={{ provider_id: f.provider_id }}
              onSubmit={(body) => enrol.mutateAsync({ body })}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={TrainingStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="Course" value={f.course_id} options={courseOptions} onChange={(v) => set("course_id", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Schemas["TrainingEnrolment"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(t) => t.id}
        empty="No enrolments."
        columns={[
          { header: "Provider", cell: (t) => (t.provider_id ? <Link className="text-primary hover:underline" to={`/providers/${t.provider_id}`}>Open file</Link> : "—") },
          { header: "Course", cell: (t) => `${t.course.code} · ${t.course.name}` },
          { header: "Status", cell: (t) => <StatusBadge value={t.status} /> },
          { header: "Scheduled", cell: (t) => dateTime(t.scheduled_for) },
          { header: "Attended", cell: (t) => dateTime(t.attended_at) },
          { header: "Score", cell: (t) => score(t.score, 0) },
          {
            header: "",
            cell: (t) => (
              <Allowed when={can.result && ["scheduled", "attended"].includes(t.status)}>
                <FormDialog
                  trigger="Record result"
                  title="Record attendance and result"
                  description={t.course.pass_mark ? `Pass mark ${t.course.pass_mark}.` : undefined}
                  fields={[
                    { name: "status", label: "Result", type: "select", options: ["attended", "passed", "failed", "no_show", "cancelled"], required: true },
                    { name: "score", label: "Score", type: "number" },
                    { name: "attended_at", label: "Attended at", type: "datetime" },
                  ]}
                  initial={{ attended_at: t.scheduled_for }}
                  onSubmit={(body) => result.mutateAsync({ params: { path: { id: t.id } }, body })}
                />
              </Allowed>
            ),
          },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
