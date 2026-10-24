import { XIcon } from "lucide-react";
import { Link } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { PaymentMethod, PaymentStatus, RefundStatus, EarningStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { PersonFilter } from "@/components/person-filter";
import { EnumFilter, FilterBar, FilterField, TextFilter } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan, useMe } from "@/lib/access";
import { dateTime, money } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function ManualPaymentDialog({ invoiceId, balance }: { invoiceId?: string; balance?: number }) {
  const manual = $api.useMutation("post", "/console/payments/manual");
  return (
    <FormDialog
      trigger="Record payment"
      triggerVariant="default"
      title="Record a payment received outside STK push"
      description="For Paybill payments that did not match automatically, bank transfers and cash."
      fields={[
        { name: "invoice_id", label: "Invoice ID", required: true },
        { name: "method", label: "Method", type: "select", options: ["mpesa_paybill", "bank_transfer", "manual"], required: true },
        { name: "amount_minor", label: "Amount (KES)", type: "money", required: true },
        { name: "reference", label: "Reference (M-Pesa receipt or bank ref)", required: true },
        { name: "received_at", label: "Received at", type: "datetime" },
      ]}
      initial={{ invoice_id: invoiceId, method: "mpesa_paybill", amount_minor: balance }}
      onSubmit={(body) => manual.mutateAsync({ body })}
    />
  );
}

export function PaymentsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status", "method", "from", "to", "client_id", "invoice_id"], pager.reset);
  const requery = $api.useMutation("post", "/console/payments/{id}/requery");
  const can = { manual: useCan("POST /console/payments/manual"), requery: useCan("POST /console/payments/{id}/requery") };
  const { data, isLoading, error } = $api.useQuery("get", "/console/payments", {
    params: {
      query: {
        status: f.status as Schemas["PaymentStatus"] | undefined,
        method: f.method as Schemas["PaymentMethod"] | undefined,
        client_id: f.client_id,
        invoice_id: f.invoice_id,
        from: f.from,
        to: f.to,
        cursor: pager.cursor,
      },
    },
  });
  return (
    <>
      <PageHeader
        title="Payments"
        description="M-Pesa STK, Paybill and manual payments against invoices."
        actions={<Allowed when={can.manual}><ManualPaymentDialog /></Allowed>}
      />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={PaymentStatus} onChange={(v) => set("status", v)} />
        <EnumFilter label="Method" value={f.method} options={PaymentMethod} onChange={(v) => set("method", v)} />
        <PersonFilter label="Client" kind="client" value={f.client_id} onChange={(v) => set("client_id", v)} />
        {f.invoice_id && (
          <FilterField label="Invoice">
            <span className="border-input flex h-9 items-center gap-2 rounded-md border px-3 text-sm">
              {data?.data[0]?.invoice_reference ?? "One invoice"}
              <button type="button" className="text-muted-foreground hover:text-foreground ml-auto" aria-label="Clear invoice" onClick={() => set("invoice_id", undefined)}>
                <XIcon className="size-4" />
              </button>
            </span>
          </FilterField>
        )}
        <TextFilter label="From" type="date" value={f.from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={f.to} onChange={(v) => set("to", v)} />
      </FilterBar>
      <DataTable<Schemas["Payment"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(p) => p.id}
        empty="No payments match these filters."
        columns={[
          { header: "Initiated", cell: (p) => dateTime(p.initiated_at) },
          {
            header: "Invoice",
            cell: (p) => (
              <Link className="text-primary font-mono text-xs hover:underline" to={`/invoices/${p.invoice_id}`}>
                {p.invoice_reference ?? "Open"}
              </Link>
            ),
          },
          {
            header: "Client",
            cell: (p) =>
              p.client_id ? (
                <Link className="text-primary hover:underline" to={`/clients/${p.client_id}`}>
                  {p.client_name ?? "Open"}
                </Link>
              ) : (
                "—"
              ),
          },
          { header: "Method", cell: (p) => <StatusBadge value={p.method} /> },
          { header: "Status", cell: (p) => <StatusBadge value={p.status} /> },
          { header: "Amount", cell: (p) => money(p.amount_minor), className: "text-right tabular-nums" },
          { header: "Payer", cell: (p) => p.payer_msisdn ?? "—" },
          { header: "Receipt", cell: (p) => <span className="font-mono text-xs">{p.mpesa_receipt_number ?? "—"}</span> },
          { header: "Failure", cell: (p) => <span className="text-xs">{p.failure_reason ?? ""}</span> },
          { header: "Completed", cell: (p) => dateTime(p.completed_at) },
          {
            header: "",
            cell: (p) => (
              <Allowed when={can.requery && ["initiated", "pending"].includes(p.status) && p.method === "mpesa_stk"}>
                <FormDialog
                  trigger="Re-query M-Pesa"
                  title="Ask M-Pesa for this payment's status"
                  description="Use when the callback never arrived."
                  submitLabel="Re-query"
                  onSubmit={() => requery.mutateAsync({ params: { path: { id: p.id } } })}
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

export function RefundsPage() {
  const me = useMe();
  const pager = useCursor();
  const [f, set] = useParamState(["status", "new", "complaint_id"], pager.reset);
  const create = $api.useMutation("post", "/console/refunds");
  const approve = $api.useMutation("post", "/console/refunds/{id}/approve");
  const reject = $api.useMutation("post", "/console/refunds/{id}/reject");
  const can = {
    create: useCan("POST /console/refunds"),
    approve: useCan("POST /console/refunds/{id}/approve"),
    reject: useCan("POST /console/refunds/{id}/reject"),
  };
  const { data, isLoading, error } = $api.useQuery("get", "/console/refunds", {
    params: { query: { status: f.status as Schemas["RefundStatus"] | undefined, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader
        title="Refunds"
        description="The officer who approves a refund must be different from the one who requested it."
        actions={
          <Allowed when={can.create}>
            <FormDialog
              trigger="Request refund"
              triggerVariant="default"
              title="Request a refund"
              autoOpen={f.new === "1"}
              fields={[
                { name: "payment_id", label: "Payment ID", required: true },
                { name: "amount_minor", label: "Amount (KES)", type: "money", required: true },
                { name: "reason", label: "Reason", type: "textarea", required: true },
                { name: "complaint_id", label: "Complaint ID" },
              ]}
              initial={{ complaint_id: f.complaint_id }}
              onSubmit={(body) => create.mutateAsync({ body })}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={RefundStatus} onChange={(v) => set("status", v)} />
      </FilterBar>
      <DataTable<Schemas["Refund"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(r) => r.id}
        empty="No refunds."
        columns={[
          { header: "Requested", cell: (r) => dateTime(r.created_at) },
          { header: "Status", cell: (r) => <StatusBadge value={r.status} /> },
          { header: "Amount", cell: (r) => money(r.amount_minor), className: "text-right tabular-nums" },
          { header: "Reason", cell: (r) => <span className="line-clamp-2 max-w-sm">{r.reason}</span> },
          { header: "By", cell: (r) => (r.requested_by === me?.id ? "You" : "Another officer") },
          { header: "Completed", cell: (r) => dateTime(r.completed_at) },
          {
            header: "",
            cell: (r) =>
              r.status === "requested" && (
                <div className="flex justify-end gap-1">
                  <Allowed when={can.approve}>
                    <FormDialog
                      trigger="Approve"
                      triggerVariant="default"
                      title="Approve and send refund"
                      description={
                        r.requested_by === me?.id
                          ? "You requested this refund, so another finance officer must approve it."
                          : `Sends ${money(r.amount_minor)} back to the client through M-Pesa.`
                      }
                      disabled={r.requested_by === me?.id}
                      submitLabel="Approve"
                      onSubmit={() => approve.mutateAsync({ params: { path: { id: r.id } } })}
                    />
                  </Allowed>
                  <Allowed when={can.reject}>
                    <FormDialog
                      trigger="Reject"
                      title="Reject refund"
                      fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                      destructive
                      onSubmit={(body) => reject.mutateAsync({ params: { path: { id: r.id } }, body })}
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

export function EarningsPage() {
  const pager = useCursor();
  const [f, set] = useParamState(["status", "provider_id"], pager.reset);
  const hold = $api.useMutation("post", "/console/earnings/{id}/hold");
  const release = $api.useMutation("post", "/console/earnings/{id}/release");
  const can = { hold: useCan("POST /console/earnings/{id}/hold"), release: useCan("POST /console/earnings/{id}/release") };
  const { data, isLoading, error } = $api.useQuery("get", "/console/earnings", {
    params: { query: { status: f.status as Schemas["EarningStatus"] | undefined, provider_id: f.provider_id, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader title="Provider earnings" description="Net = gross − commission − withholding. Held earnings are left out of payout batches." />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={EarningStatus} onChange={(v) => set("status", v)} />
        <PersonFilter label="Provider" kind="provider" value={f.provider_id} onChange={(v) => set("provider_id", v)} />
      </FilterBar>
      <DataTable<Schemas["Earning"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(e) => e.id}
        empty="No earnings."
        columns={[
          { header: "Earned", cell: (e) => dateTime(e.earned_at) },
          { header: "Request", cell: (e) => <span className="font-mono text-xs">{e.request_reference ?? "—"}</span> },
          { header: "Status", cell: (e) => <StatusBadge value={e.status} /> },
          { header: "Gross", cell: (e) => money(e.gross_minor), className: "text-right tabular-nums" },
          { header: "Commission", cell: (e) => money(e.commission_minor), className: "text-right tabular-nums" },
          { header: "Withholding", cell: (e) => money(e.withholding_minor), className: "text-right tabular-nums" },
          { header: "Net", cell: (e) => <span className="font-medium">{money(e.net_minor)}</span>, className: "text-right tabular-nums" },
          { header: "Hold reason", cell: (e) => <span className="text-xs">{e.hold_reason ?? ""}</span> },
          {
            header: "",
            cell: (e) => (
              <div className="flex justify-end gap-1">
                <Allowed when={can.hold && ["pending", "payable"].includes(e.status)}>
                  <FormDialog
                    trigger="Hold"
                    title="Hold this earning"
                    description="For example while a loss complaint is open."
                    fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                    onSubmit={(body) => hold.mutateAsync({ params: { path: { id: e.id } }, body })}
                  />
                </Allowed>
                <Allowed when={can.release && e.status === "held"}>
                  <FormDialog
                    trigger="Release"
                    title="Release this earning"
                    fields={[{ name: "reason", label: "Reason", type: "textarea", required: true }]}
                    onSubmit={(body) => release.mutateAsync({ params: { path: { id: e.id } }, body })}
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
