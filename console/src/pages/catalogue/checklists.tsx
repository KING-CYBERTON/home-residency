import { $api } from "@/api/client";
import { ChecklistItemKind, DocumentType } from "@/api/enums.gen";
import { Badge } from "@/components/ui/badge";
import { EnumFilter, FilterBar } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useCategoryName, useCategoryOptions } from "@/components/lookups";
import { Empty, ErrorState, Loading, PageHeader, Section } from "@/components/page";
import { useCan, useMe } from "@/lib/access";
import { dateTime, humanize } from "@/lib/format";
import { useParamState } from "@/lib/params";

const itemsHelp = (
  <>
    One item per line: <code>kind | label | mandatory | document type | validity months</code>. Kinds:{" "}
    {ChecklistItemKind.join(", ")}. Document types: {DocumentType.join(", ")}.
  </>
);

/** Parses the one-line-per-item text format into ChecklistTemplateItem objects. */
function parseItems(text: string) {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .map((line, i) => {
      const [kind, label, mandatory, documentType, validity] = line.split("|").map((s) => s.trim());
      return {
        kind,
        label,
        is_mandatory: !/^(no|false|optional)$/i.test(mandatory ?? ""),
        document_type: documentType || undefined,
        validity_months: validity ? Number(validity) : undefined,
        sort_order: i + 1,
      };
    });
}

export function ChecklistsPage() {
  const me = useMe();
  const [f, set] = useParamState(["category_id"]);
  const categories = useCategoryOptions();
  const categoryName = useCategoryName();
  const create = $api.useMutation("post", "/console/checklists");
  const publish = $api.useMutation("post", "/console/checklists/{id}/publish");
  const can = { create: useCan("POST /console/checklists"), publish: useCan("POST /console/checklists/{id}/publish") };
  const { data, isLoading, error } = $api.useQuery("get", "/console/checklists", {
    params: { query: { category_id: f.category_id } },
  });
  return (
    <>
      <PageHeader
        title="Vetting checklists"
        description="What each category's applicants must provide. Admins draft a version; an approver makes it current."
        actions={
          <Allowed when={can.create}>
            <FormDialog
              trigger="New version"
              triggerVariant="default"
              title="Draft a checklist version"
              fields={[
                { name: "category_id", label: "Category", type: "select", options: categories, required: true },
                { name: "items", label: "Items", type: "textarea", required: true, help: itemsHelp },
              ]}
              initial={{
                category_id: f.category_id,
                items: "document | National ID | yes | national_id\ndocument | Certificate of Good Conduct | yes | good_conduct_certificate | 12\nreferee | Two referees | yes\ninterview | Interview | yes",
              }}
              submitLabel="Save draft"
              onSubmit={(body) => create.mutateAsync({ body: { category_id: body.category_id, items: parseItems(body.items) } as never })}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Category" value={f.category_id} options={categories} onChange={(v) => set("category_id", v)} />
      </FilterBar>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} />
      ) : !data?.length ? (
        <Empty>No checklists yet.</Empty>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {data.map((t) => (
            <Section
              key={t.id}
              title={
                <span className="flex items-center gap-2">
                  {categoryName(t.category_id)} v{t.version}
                  {t.is_current ? <Badge variant="success">Current</Badge> : <Badge variant="secondary">Draft</Badge>}
                </span>
              }
              actions={
                <Allowed when={can.publish && !t.is_current}>
                  <FormDialog
                    trigger="Make current"
                    title={`Make v${t.version} current`}
                    description="New applications use this version. Applications already open keep theirs."
                    submitLabel="Publish"
                    onSubmit={() => publish.mutateAsync({ params: { path: { id: t.id } } })}
                  />
                </Allowed>
              }
            >
              <ol className="list-decimal space-y-1 pl-5 text-sm">
                {t.items.map((i, n) => (
                  <li key={i.id ?? n}>
                    {i.label}{" "}
                    <span className="text-muted-foreground text-xs">
                      {humanize(i.kind)}
                      {i.document_type ? ` · ${humanize(i.document_type)}` : ""}
                      {i.validity_months ? ` · valid ${i.validity_months} months` : ""}
                      {i.is_mandatory ? "" : " · optional"}
                    </span>
                  </li>
                ))}
              </ol>
              <p className="text-muted-foreground mt-3 text-xs">
                Created {dateTime(t.created_at)}
                {t.approved_by ? ` · approved${t.approved_by === me?.id ? " by you" : ""}` : ""}
              </p>
            </Section>
          ))}
        </div>
      )}
    </>
  );
}
