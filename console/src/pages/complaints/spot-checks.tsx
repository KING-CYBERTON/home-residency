import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { useCan } from "@/lib/access";
import { dateTime, humanize, score } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function SpotChecksPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["provider_id", "from", "to"], pager.reset);
  const create = $api.useMutation("post", "/console/spot-checks");
  const canCreate = useCan("POST /console/spot-checks");
  const { data, isLoading, error } = $api.useQuery("get", "/console/spot-checks", {
    params: { query: { provider_id: f.provider_id, from: f.from, to: f.to, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader
        title="Spot checks"
        description="Unannounced quality checks. Scores feed the provider's appraisal."
        actions={
          <Allowed when={canCreate}>
            <FormDialog
              trigger="Record spot check"
              triggerVariant="default"
              title="Record a spot check"
              fields={[
                { name: "provider_id", label: "Provider ID", required: true },
                { name: "visit_id", label: "Visit ID" },
                { name: "method", label: "Method", type: "select", options: ["on_site", "phone_client", "phone_provider"], required: true },
                { name: "scores", label: "Scores by criterion (JSON)", type: "json", required: true, help: 'For example {"punctuality": 5, "quality": 4}' },
                { name: "total_score", label: "Total score", type: "number", required: true },
                { name: "notes", label: "Notes", type: "textarea" },
                { name: "raise_complaint", label: "Raise a complaint from this check", type: "checkbox" },
              ]}
              initial={{ provider_id: f.provider_id, method: "on_site", scores: {} }}
              onSubmit={(body) => create.mutateAsync({ body })}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <TextFilter label="Provider ID" value={f.provider_id} onChange={(v) => set("provider_id", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Schemas["SpotCheck"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(s) => s.id ?? `${s.provider_id}-${s.checked_at}`}
        empty="No spot checks."
        columns={[
          { header: "Checked", cell: (s) => dateTime(s.checked_at) },
          { header: "Provider", cell: (s) => <Link className="text-primary hover:underline" to={`/providers/${s.provider_id}`}>Open file</Link> },
          { header: "Method", cell: (s) => humanize(s.method) },
          { header: "Score", cell: (s) => score(s.total_score), className: "tabular-nums" },
          { header: "Scores", cell: (s) => <span className="text-xs">{Object.entries(s.scores ?? {}).map(([k, v]) => `${k} ${String(v)}`).join(", ")}</span> },
          { header: "Notes", cell: (s) => <span className="line-clamp-2 max-w-xs text-xs">{s.notes}</span> },
          { header: "Complaint", cell: (s) => (s.complaint_id ? <Link className="text-primary hover:underline" to={`/complaints/${s.complaint_id}`}>Open</Link> : "—") },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
