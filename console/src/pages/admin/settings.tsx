import { $api, type Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { useCan } from "@/lib/access";
import { dateTime } from "@/lib/format";

export function SettingsPage() {
  const { data, isLoading, error } = $api.useQuery("get", "/console/settings", {});
  const put = $api.useMutation("put", "/console/settings/{key}");
  const canEdit = useCan("PUT /console/settings/{key}");
  return (
    <>
      <PageHeader
        title="Settings"
        description="Tunable business rules: rating threshold, offer expiry, check-in radius, appraisal weights, payout cycle and more. Every change is audited."
      />
      <DataTable<Schemas["Setting"]>
        rows={data}
        loading={isLoading}
        error={error}
        rowKey={(s) => s.key}
        empty="No settings."
        columns={[
          { header: "Key", cell: (s) => <code className="text-xs">{s.key}</code> },
          { header: "Value", cell: (s) => <code className="bg-muted rounded px-1.5 py-0.5 text-xs">{JSON.stringify(s.value)}</code> },
          { header: "What it does", cell: (s) => <span className="text-muted-foreground text-xs">{s.description}</span> },
          { header: "Changed", cell: (s) => dateTime(s.updated_at) },
          {
            header: "",
            cell: (s) => (
              <Allowed when={canEdit}>
                <FormDialog
                  trigger="Change"
                  title={`Change ${s.key}`}
                  description={s.description ?? undefined}
                  fields={[{ name: "value", label: "Value (JSON)", type: "json", required: true, help: 'Numbers as 4.5, text in quotes "like this", objects as {"a": 1}.' }]}
                  initial={{ value: s.value }}
                  submitLabel="Save"
                  onSubmit={(body) => put.mutateAsync({ params: { path: { key: s.key } }, body })}
                />
              </Allowed>
            ),
          },
        ]}
      />
    </>
  );
}
