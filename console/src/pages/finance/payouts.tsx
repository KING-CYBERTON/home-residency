import { Link, useNavigate, useParams } from "react-router";

import { $api, type Schemas } from "@/api/client";
import { PayoutStatus } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { DetailList, ErrorState, Loading, PageHeader, Section } from "@/components/page";
import { Pager } from "@/components/pager";
import { useCursor } from "@/hooks/use-cursor";
import { StatusBadge } from "@/components/status-badge";
import { useCan, useMe } from "@/lib/access";
import { date, dateTime, daysAgo, isoDate, money } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function PayoutBatchesPage() {
  const navigate = useNavigate();
  const pager = useCursor();
  const [f, set] = useParamState(["status"], pager.reset);
  const prepare = $api.useMutation("post", "/console/payout-batches");
  const canPrepare = useCan("POST /console/payout-batches");
  const { data, isLoading, error } = $api.useQuery("get", "/console/payout-batches", {
    params: { query: { status: f.status as Schemas["PayoutStatus"] | undefined, cursor: pager.cursor } },
  });
  return (
    <>
      <PageHeader
        title="Payout batches"
        description="Weekly M-Pesa B2C payouts to providers. The officer who approves a batch must be different from the one who prepared it."
        actions={
          <Allowed when={canPrepare}>
            <FormDialog
              trigger="Prepare batch"
              triggerVariant="default"
              title="Prepare a payout batch"
              description="Collects every payable earning in the cycle that is not on hold."
              fields={[
                { name: "cycle_start", label: "Cycle start", type: "date", required: true },
                { name: "cycle_end", label: "Cycle end", type: "date", required: true },
              ]}
              initial={{ cycle_start: daysAgo(7), cycle_end: isoDate(new Date()) }}
              submitLabel="Prepare"
              onSubmit={(body) => prepare.mutateAsync({ body })}
              onDone={(b) => navigate(`/payout-batches/${(b as Schemas["PayoutBatch"]).id}`)}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Status" value={f.status} options={PayoutStatus} onChange={(v) => set("status", v)} />
      </FilterBar>
      <DataTable<Schemas["PayoutBatch"]>
        rows={data?.data}
        loading={isLoading}
        error={error}
        rowKey={(b) => b.id}
        rowHref={(b) => `/payout-batches/${b.id}`}
        empty="No payout batches."
        columns={[
          { header: "Cycle", cell: (b) => `${date(b.cycle_start)} – ${date(b.cycle_end)}` },
          { header: "Status", cell: (b) => <StatusBadge value={b.status} /> },
          { header: "Payouts", cell: (b) => b.payout_count ?? "—", className: "tabular-nums" },
          { header: "Total", cell: (b) => money(b.total_minor), className: "text-right tabular-nums" },
          { header: "Prepared", cell: (b) => dateTime(b.created_at) },
          { header: "Approved", cell: (b) => dateTime(b.approved_at) },
        ]}
      />
      <Pager pager={pager} nextCursor={data?.meta.next_cursor} />
    </>
  );
}

export function PayoutBatchPage() {
  const { id = "" } = useParams();
  const me = useMe();
  const path = { params: { path: { id } } };
  const q = $api.useQuery("get", "/console/payout-batches/{id}", path, { refetchInterval: 15_000 });
  const approve = $api.useMutation("post", "/console/payout-batches/{id}/approve");
  const retry = $api.useMutation("post", "/console/payouts/{id}/retry");
  const can = { approve: useCan("POST /console/payout-batches/{id}/approve"), retry: useCan("POST /console/payouts/{id}/retry") };
  if (q.isLoading) return <Loading />;
  if (q.error || !q.data) return <ErrorState error={q.error} />;
  const b = q.data;
  const preparedByMe = b.prepared_by === me?.id;
  return (
    <>
      <PageHeader
        back={{ to: "/payout-batches", label: "Payout batches" }}
        title={
          <span className="flex items-center gap-3">
            Payouts {date(b.cycle_start)} – {date(b.cycle_end)} <StatusBadge value={b.status} />
          </span>
        }
        actions={
          <Allowed when={can.approve && b.status === "draft"}>
            <FormDialog
              trigger="Approve and send"
              triggerVariant="default"
              title="Approve and send payouts"
              description={
                preparedByMe
                  ? "You prepared this batch, so another finance officer must approve it."
                  : `Sends ${money(b.total_minor)} to ${b.payout_count ?? b.payouts?.length ?? 0} providers through M-Pesa B2C. This cannot be undone.`
              }
              disabled={preparedByMe}
              submitLabel="Approve and send"
              onSubmit={() => approve.mutateAsync(path)}
            />
          </Allowed>
        }
      />
      <Section title="Summary">
        <DetailList
          items={[
            ["Total", <span className="font-semibold">{money(b.total_minor)}</span>],
            ["Payouts", b.payout_count ?? b.payouts?.length ?? 0],
            ["Prepared", `${dateTime(b.created_at)}${preparedByMe ? " by you" : ""}`],
            ["Approved", dateTime(b.approved_at)],
          ]}
        />
      </Section>
      <Section className="mt-4" title="Payouts">
        <DataTable<Schemas["Payout"]>
          rows={b.payouts}
          rowKey={(p) => p.id}
          empty="No payouts in this batch."
          columns={[
            { header: "Provider", cell: (p) => (p.provider_id ? <Link className="text-primary hover:underline" to={`/providers/${p.provider_id}`}>Open file</Link> : "—") },
            { header: "Number", cell: (p) => p.msisdn ?? "—" },
            { header: "Amount", cell: (p) => money(p.amount_minor), className: "text-right tabular-nums" },
            { header: "Status", cell: (p) => <StatusBadge value={p.status} /> },
            { header: "Receipt", cell: (p) => <span className="font-mono text-xs">{p.mpesa_receipt_number ?? "—"}</span> },
            { header: "Failure", cell: (p) => <span className="text-xs">{p.failure_reason ?? ""}</span> },
            { header: "Paid", cell: (p) => dateTime(p.paid_at) },
            {
              header: "",
              cell: (p) => (
                <Allowed when={can.retry && p.status === "failed"}>
                  <FormDialog
                    trigger="Retry"
                    title="Retry this payout"
                    description="Check the provider's payout number first if the failure was an invalid account."
                    submitLabel="Retry"
                    onSubmit={() => retry.mutateAsync({ params: { path: { id: p.id } } })}
                  />
                </Allowed>
              ),
            },
          ]}
        />
      </Section>
    </>
  );
}
