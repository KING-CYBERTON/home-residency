import { Link, useParams } from "react-router";

import { $api, type Schemas } from "@/api/client";

import { DataTable } from "@/components/data-table";
import { DocumentButton } from "@/components/document-button";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan } from "@/lib/access";
import { date, dateTime, humanize } from "@/lib/format";

/** View, verify, reject and audit one vault document. Used on application and provider files. */
export function DocumentActions({ doc, purpose }: { doc: Schemas["Document"]; purpose: string }) {
  const verify = $api.useMutation("post", "/console/documents/{id}/verify");
  const reject = $api.useMutation("post", "/console/documents/{id}/reject");
  const can = {
    verify: useCan("POST /console/documents/{id}/verify"),
    reject: useCan("POST /console/documents/{id}/reject"),
    log: useCan("GET /console/documents/{id}/access-log"),
  };
  const path = { params: { path: { id: doc.id } } };
  const reviewable = doc.status === "uploaded" || doc.status === "verified";
  return (
    <div className="flex flex-wrap items-center justify-end gap-1">
      {doc.status !== "pending_upload" && doc.status !== "purged" && <DocumentButton id={doc.id} purpose={purpose} />}
      <Allowed when={can.verify && reviewable && doc.status !== "verified"}>
        <FormDialog
          trigger="Verify"
          title={`Verify ${humanize(doc.document_type)}`}
          description="Confirm the document is genuine and record its dates. Expiry dates drive automatic suspension."
          fields={[
            { name: "issued_on", label: "Issued on", type: "date" },
            { name: "expires_on", label: "Expires on", type: "date" },
            { name: "notes", label: "Notes", type: "textarea" },
          ]}
          initial={{ issued_on: doc.issued_on, expires_on: doc.expires_on }}
          onSubmit={(body) => verify.mutateAsync({ ...path, body })}
        />
      </Allowed>
      <Allowed when={can.reject && reviewable}>
        <FormDialog
          trigger="Reject"
          title={`Reject ${humanize(doc.document_type)}`}
          fields={[{ name: "reason", label: "Reason (shown to the provider)", type: "textarea", required: true }]}
          destructive
          onSubmit={(body) => reject.mutateAsync({ ...path, body })}
        />
      </Allowed>
      <Allowed when={can.log}>
        <Link className="text-primary px-2 text-xs hover:underline" to={`/documents/${doc.id}/access-log`}>
          Access log
        </Link>
      </Allowed>
    </div>
  );
}

export function DocumentsTable({ docs, purpose }: { docs: Schemas["Document"][] | undefined; purpose: string }) {
  return (
    <DataTable<Schemas["Document"]>
      rows={docs}
      rowKey={(d) => d.id}
      empty="No documents."
      columns={[
        { header: "Type", cell: (d) => humanize(d.document_type) },
        { header: "Status", cell: (d) => <StatusBadge value={d.status} /> },
        { header: "File", cell: (d) => <span className="text-xs">{d.original_filename ?? "—"}</span> },
        { header: "Issued", cell: (d) => date(d.issued_on) },
        { header: "Expires", cell: (d) => date(d.expires_on) },
        { header: "Verified", cell: (d) => dateTime(d.verified_at) },
        { header: "", cell: (d) => <DocumentActions doc={d} purpose={purpose} /> },
      ]}
    />
  );
}

export function AccessLogPage() {
  const { id = "" } = useParams();
  const pager = useCursor();
  const doc = $api.useQuery("get", "/documents/{id}", { params: { path: { id } } });
  const { data, isLoading, error } = $api.useQuery("get", "/console/documents/{id}/access-log", {
    params: { path: { id } },
  });
  return (
    <>
      <PageHeader
        title="Document access log"
        description={doc.data ? `${humanize(doc.data.document_type)} · ${doc.data.original_filename ?? id}` : id}
      />
      <DataTable<Schemas["AccessLogEntry"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(e) => e.id}
        empty="Nobody has opened this document."
        columns={[
          { header: "When", cell: (e) => dateTime(e.accessed_at) },
          { header: "Who", cell: (e) => <span className="font-mono text-xs">{e.accessed_by}</span> },
          { header: "Purpose", cell: (e) => e.purpose ?? "—" },
          { header: "IP address", cell: (e) => e.ip_address ?? "—" },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

