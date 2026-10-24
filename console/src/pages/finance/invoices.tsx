import { Link, useParams } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { InvoiceStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { PersonFilter } from "@/components/person-filter";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { DetailList, ErrorState, Loading, PageHeader, Section } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan } from "@/lib/access";
import { date, dateTime, humanize, money } from "@/lib/format";
import { useParamState } from "@/lib/params";

import { ManualPaymentDialog } from "./payments";

export function InvoicesPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status", "client_id", "from", "to"], pager.reset);
  const { data, isLoading, error } = $api.useQuery("get", "/console/invoices", {
    params: {
      query: {
        status: f.status as Schemas["InvoiceStatus"] | undefined,
        client_id: f.client_id,
        from: f.from,
        to: f.to,
        cursor: pager.cursor,
      },
    },
  });
  return (
    <>
      <PageHeader title="Invoices" description="Client invoices for jobs, placement fees, variations and subscriptions. Amounts include VAT." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={InvoiceStatus} onChange={(v) => set("status", v)} />
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
        <PersonFilter label="Client" kind="client" value={f.client_id} onChange={(v) => set("client_id", v)} />
      </FilterBar>
      <DataTable<Schemas["Invoice"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(i) => i.id}
        rowHref={(i) => `/invoices/${i.id}`}
        empty="No invoices match these filters."
        columns={[
          { header: "Reference", cell: (i) => <span className="font-mono text-xs">{i.reference}</span> },
          { header: "Client", cell: (i) => i.client_name ?? "—" },
          { header: "Status", cell: (i) => <StatusBadge value={i.status} /> },
          { header: "Issued", cell: (i) => date(i.issued_at) },
          { header: "Due", cell: (i) => date(i.due_at) },
          { header: "Total", cell: (i) => money(i.total_minor), className: "text-right tabular-nums" },
          { header: "Paid", cell: (i) => money(i.paid_minor), className: "text-right tabular-nums" },
          { header: "Balance", cell: (i) => money(i.total_minor - i.paid_minor), className: "text-right tabular-nums" },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

export function InvoicePage() {
  const { id = "" } = useParams();
  const path = { params: { path: { id } } };
  const q = $api.useQuery("get", "/console/invoices/{id}", path);
  const voidInv = $api.useMutation("post", "/console/invoices/{id}/void");
  const can = { void: useCan("POST /console/invoices/{id}/void"), manual: useCan("POST /console/payments/manual") };
  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const i = q.data;
  const unpaid = ["draft", "issued", "partially_paid"].includes(i.status);
  return (
    <>
      <PageHeader
        back={{ to: "/invoices", label: "Invoices" }}
        title={
          <span className="flex items-center gap-3">
            <span className="font-mono">{i.reference}</span>
            <StatusBadge value={i.status} />
          </span>
        }
        actions={
          <>
            <Allowed when={can.manual && unpaid}>
              <ManualPaymentDialog invoiceId={i.id} balance={i.total_minor - i.paid_minor} />
            </Allowed>
            <Allowed when={can.void && i.paid_minor === 0 && unpaid}>
              <FormDialog
                trigger="Void"
                title="Void this invoice"
                fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                destructive
                onSubmit={(body) => voidInv.mutateAsync({ ...path, body })}
              />
            </Allowed>
          </>
        }
      />
      <div className="grid gap-4 lg:grid-cols-3">
        <Section title="Lines" className="lg:col-span-2">
          <DataTable<Schemas["InvoiceLine"]>
            rows={i.lines}
            rowKey={(l) => l.id ?? l.description}
            empty="No lines."
            columns={[
              { header: "Kind", cell: (l) => humanize(l.kind) },
              { header: "Description", cell: (l) => l.description },
              { header: "Qty", cell: (l) => l.quantity ?? 1, className: "text-right tabular-nums" },
              { header: "Unit", cell: (l) => money(l.unit_minor), className: "text-right tabular-nums" },
              { header: "Amount", cell: (l) => money(l.amount_minor), className: "text-right tabular-nums" },
            ]}
          />
        </Section>
        <Section title="Summary">
          <DetailList
            className="sm:grid-cols-1"
            items={[
              ["Total", <span className="font-semibold">{money(i.total_minor)}</span>],
              ["Includes VAT", money(i.vat_minor)],
              ["Net of VAT", money(i.subtotal_minor)],
              ["Paid", money(i.paid_minor)],
              ["Status", <StatusBadge value={i.status} />],
              ["Issued", dateTime(i.issued_at)],
              ["Due", date(i.due_at)],
              ["Paid in full", dateTime(i.paid_at)],
              ["Request", i.request_id ? <Link className="text-primary hover:underline" to={`/requests/${i.request_id}`}>Open request</Link> : "—"],
              ["Client", i.client_id ? <Link className="text-primary hover:underline" to={`/clients/${i.client_id}`}>{i.client_name ?? "Open client"}</Link> : "—"],
              ["Payments", <Link className="text-primary hover:underline" to={`/payments?invoice_id=${i.id}`}>See payments</Link>],
            ]}
          />
        </Section>
      </div>
    </>
  );
}
