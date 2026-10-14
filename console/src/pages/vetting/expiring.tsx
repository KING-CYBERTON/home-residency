import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useCategoryOptions } from "@/components/lookups";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { useCan } from "@/lib/access";
import { date, humanize } from "@/lib/format";
import { useParamState } from "@/lib/params";
import { cn } from "@/lib/utils";

import { DocumentActions } from "./documents";

export function ExpiringDocumentsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["within_days"], pager.reset);
  const within = Number(f.within_days ?? 30);
  const categories = useCategoryOptions();
  const reverify = $api.useMutation("post", "/console/providers/{id}/reverification");
  const canReverify = useCan("POST /console/providers/{id}/reverification");
  const { data, isLoading, error } = $api.useQuery("get", "/console/compliance/expiring-documents", {
    params: { query: { within_days: within } },
  });
  return (
    <>
      <PageHeader
        title="Expiring documents"
        description="Mandatory documents close to expiry. When one expires the provider is suspended automatically by the nightly job."
      />
      <FilterBar>
        <TextFilter label="Within (days)" type="number" value={String(within)} onChange={(v) => set("within_days", v)} />
      </FilterBar>
      <DataTable<Schemas["ExpiringDocument"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(e) => e.document.id}
        empty="No documents expire in this window."
        columns={[
          { header: "Provider", cell: (e) => <Link className="text-primary hover:underline" to={`/providers/${e.provider.id}`}>{e.provider.full_name}</Link> },
          { header: "Document", cell: (e) => humanize(e.document.document_type) },
          { header: "Expires", cell: (e) => date(e.document.expires_on) },
          {
            header: "Days left",
            cell: (e) => <span className={cn("font-medium tabular-nums", e.days_left <= 7 && "text-destructive")}>{e.days_left}</span>,
          },
          {
            header: "",
            cell: (e) => (
              <div className="flex justify-end gap-1">
                <DocumentActions doc={e.document} purpose="Compliance expiry review" />
                <Allowed when={canReverify}>
                  <FormDialog
                    trigger="Re-verify"
                    title={`Open re-verification for ${e.provider.full_name}`}
                    description="Creates a re-verification application so the provider can upload a renewed document."
                    fields={[{ name: "category_id", label: "Category", type: "select", options: categories, required: true }]}
                    onSubmit={(body) => reverify.mutateAsync({ params: { path: { id: e.provider.id } }, body })}
                  />
                </Allowed>
              </div>
            ),
          },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}
