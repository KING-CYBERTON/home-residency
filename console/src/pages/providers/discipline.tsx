import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { StatusBadge } from "@/components/status-badge";
import { useCan, useMe } from "@/lib/access";
import { dateTime } from "@/lib/format";

/** Disciplinary ladder entries. A disqualification needs a second, different officer to authorise it. */
export function DisciplinaryTable({ actions }: { actions: Schemas["DisciplinaryAction"][] | undefined }) {
  const me = useMe();
  const authorise = $api.useMutation("post", "/console/disciplinary-actions/{id}/authorise");
  const revoke = $api.useMutation("post", "/console/disciplinary-actions/{id}/revoke");
  const can = {
    authorise: useCan("POST /console/disciplinary-actions/{id}/authorise"),
    revoke: useCan("POST /console/disciplinary-actions/{id}/revoke"),
  };
  return (
    <DataTable<Schemas["DisciplinaryAction"]>
      rows={actions}
      rowKey={(d) => d.id}
      empty="No disciplinary actions."
      columns={[
        { header: "Action", cell: (d) => <StatusBadge value={d.kind} /> },
        { header: "Status", cell: (d) => <StatusBadge value={d.status} /> },
        { header: "Reason", cell: (d) => <span className="line-clamp-2 max-w-sm">{d.reason}</span> },
        { header: "From", cell: (d) => dateTime(d.effective_from) },
        { header: "Until", cell: (d) => dateTime(d.effective_to) },
        { header: "Complaint", cell: (d) => (d.complaint_id ? <Link className="text-primary hover:underline" to={`/complaints/${d.complaint_id}`}>Open</Link> : "—") },
        {
          header: "",
          cell: (d) => (
            <div className="flex justify-end gap-1">
              <Allowed when={can.authorise && d.status === "pending_authorisation"}>
                <FormDialog
                  trigger="Authorise"
                  triggerVariant="destructive"
                  title="Authorise disqualification"
                  description={
                    d.decided_by === me?.id
                      ? "You proposed this disqualification. A second, different officer must authorise it."
                      : "As the second officer, you confirm the provider is disqualified. This cannot be undone except on appeal."
                  }
                  disabled={d.decided_by === me?.id}
                  destructive
                  submitLabel="Authorise"
                  onSubmit={() => authorise.mutateAsync({ params: { path: { id: d.id } } })}
                />
              </Allowed>
              <Allowed when={can.revoke && ["pending_authorisation", "in_effect"].includes(d.status)}>
                <FormDialog
                  trigger="Revoke"
                  title="Revoke this action"
                  description="For example when an appeal succeeds."
                  fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                  onSubmit={(body) => revoke.mutateAsync({ params: { path: { id: d.id } }, body })}
                />
              </Allowed>
            </div>
          ),
        },
      ]}
    />
  );
}
