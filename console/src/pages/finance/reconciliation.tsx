import { $api, type Schemas } from "@/api/client";
import { Card } from "@/components/ui/card";
import { DataTable } from "@/components/data-table";
import { FilterBar, TextFilter } from "@/components/filters";
import { Empty, ErrorState, PageHeader, Section } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { daysAgo, dateTime, humanize, isoDate, money } from "@/lib/format";
import { useParamState } from "@/lib/params";
import { cn } from "@/lib/utils";

export function ReconciliationPage() {
  const [f, set] = useParamState(["from", "to"]);
  const from = f.from ?? daysAgo(30);
  const to = f.to ?? isoDate(new Date());
  const balances = $api.useQuery("get", "/console/ledger/balances", {});
  const rec = $api.useQuery("get", "/console/reconciliation", { params: { query: { from, to } } });
  const r = rec.data;
  const unmatchedMpesa = (r?.unmatched_mpesa ?? []) as Record<string, unknown>[];
  return (
    <>
      <PageHeader
        title="Ledger and reconciliation"
        description="Client money sits in a segregated account. The double-entry ledger must agree with what M-Pesa reports."
      />
      <Section title="Ledger balances">
        {balances.error ? (
          <ErrorState error={balances.error} />
        ) : (
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {(balances.data ?? []).map((b) => (
              <Card key={b.account} className="gap-1 px-4 py-3">
                <div className="text-muted-foreground text-xs">{humanize(b.account)}</div>
                <div className={cn("text-lg font-semibold tabular-nums", b.balance_minor < 0 && "text-destructive")}>{money(b.balance_minor)}</div>
              </Card>
            ))}
          </div>
        )}
      </Section>

      <div className="mt-6">
        <FilterBar>
          <TextFilter label="From" type="date" value={from} onChange={(v) => set("from", v)} />
          <TextFilter label="To" type="date" value={to} onChange={(v) => set("to", v)} />
        </FilterBar>
      </div>
      {rec.error ? (
        <ErrorState error={rec.error} />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
            <Stat label="Matched" value={r?.matched ?? "…"} />
            <Stat label="Unmatched M-Pesa" value={unmatchedMpesa.length} bad={unmatchedMpesa.length > 0} />
            <Stat label="Unmatched payments" value={r?.unmatched_payments?.length ?? 0} bad={(r?.unmatched_payments?.length ?? 0) > 0} />
          </div>
          <Section className="mt-4" title="M-Pesa transactions with no matching payment">
            {unmatchedMpesa.length ? (
              <div className="space-y-2">
                {unmatchedMpesa.map((m, i) => (
                  <pre key={i} className="bg-muted/50 overflow-x-auto rounded-md p-2 text-xs">
                    {JSON.stringify(m, null, 2)}
                  </pre>
                ))}
              </div>
            ) : (
              <Empty>Everything M-Pesa reported is matched.</Empty>
            )}
          </Section>
          <Section className="mt-4" title="Payments with no matching M-Pesa transaction">
            <DataTable<Schemas["Payment"]>
              rows={r?.unmatched_payments}
              loading={rec.isLoading}
              rowKey={(p) => p.id}
              empty="Every payment is matched."
              columns={[
                { header: "Initiated", cell: (p) => dateTime(p.initiated_at) },
                { header: "Method", cell: (p) => humanize(p.method) },
                { header: "Status", cell: (p) => <StatusBadge value={p.status} /> },
                { header: "Amount", cell: (p) => money(p.amount_minor), className: "text-right tabular-nums" },
                { header: "Receipt", cell: (p) => <span className="font-mono text-xs">{p.mpesa_receipt_number ?? "—"}</span> },
              ]}
            />
          </Section>
          <p className="text-muted-foreground mt-4 text-xs">Provider payable at period end: {money(r?.provider_payable_minor)}</p>
        </>
      )}
    </>
  );
}

function Stat({ label, value, bad }: { label: string; value: React.ReactNode; bad?: boolean }) {
  return (
    <Card className={cn("gap-1 px-4 py-3", bad && "border-destructive/50")}>
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className={cn("text-lg font-semibold tabular-nums", bad && "text-destructive")}>{value}</div>
    </Card>
  );
}
