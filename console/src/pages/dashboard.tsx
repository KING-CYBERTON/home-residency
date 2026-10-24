import { Link } from "react-router";

import { Card } from "@/components/ui/card";
import { $api } from "@/api/client";
import { ErrorState, PageHeader } from "@/components/page";
import { canCall, useMe } from "@/lib/access";
import { cn } from "@/lib/utils";

type Tile = {
  key: keyof NonNullable<ReturnType<typeof useDashboard>["data"]>;
  label: string;
  to: string;
  op: string;
  urgent?: boolean;
};

const tiles: Tile[] = [
  { key: "triage_queue", label: "Requests to triage", to: "/requests?status=submitted", op: "GET /console/requests" },
  { key: "awaiting_provider", label: "Waiting on a provider", to: "/requests?status=awaiting_provider", op: "GET /console/requests" },
  { key: "todays_visits", label: "Visits today", to: "/requests?status=confirmed", op: "GET /console/requests" },
  { key: "open_exceptions", label: "Open exceptions", to: "/exceptions", op: "GET /console/exceptions" },
  { key: "new_leads", label: "New website enquiries", to: "/leads?status=new", op: "GET /console/leads" },
  { key: "applications_awaiting_vetting", label: "Applications to vet", to: "/applications?status=submitted", op: "GET /console/applications" },
  { key: "applications_awaiting_approval", label: "Awaiting approval", to: "/applications?status=awaiting_approval", op: "GET /console/applications" },
  { key: "documents_expiring_30d", label: "Documents expiring in 30 days", to: "/expiring-documents", op: "GET /console/compliance/expiring-documents" },
  { key: "open_complaints", label: "Open complaints", to: "/complaints", op: "GET /console/complaints" },
  { key: "open_safeguarding_complaints", label: "Open safeguarding cases", to: "/complaints?severity=safeguarding", op: "GET /console/complaints", urgent: true },
  { key: "payouts_awaiting_approval", label: "Payout batches to approve", to: "/payout-batches?status=draft", op: "GET /console/payout-batches" },
];

function useDashboard() {
  return $api.useQuery("get", "/console/dashboard", {}, { refetchInterval: 30_000 });
}

export function DashboardPage() {
  const me = useMe();
  const { data, error, isLoading } = useDashboard();
  const visible = tiles.filter((t) => canCall(me?.roles, t.op));
  return (
    <>
      <PageHeader title={`Good ${greeting()}, ${me?.full_name?.split(" ")[0] ?? ""}`} description="Work waiting across the queues you can see." />
      {error ? (
        <ErrorState error={error} />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {visible.map((t) => {
            const n = data?.[t.key] ?? 0;
            return (
              <Link key={t.key} to={t.to}>
                <Card
                  className={cn(
                    "hover:border-primary/50 gap-1 px-5 py-4 transition-colors",
                    t.urgent && n > 0 && "border-destructive/50 bg-destructive/5",
                  )}
                >
                  <div className={cn("text-3xl font-semibold tabular-nums", t.urgent && n > 0 && "text-destructive")}>
                    {isLoading ? "…" : n}
                  </div>
                  <div className="text-muted-foreground text-sm">{t.label}</div>
                </Card>
              </Link>
            );
          })}
        </div>
      )}
    </>
  );
}

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "morning" : h < 17 ? "afternoon" : "evening";
}
