import { Download } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { $api, download, errorMessage } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { useAreaOptions, useCategoryOptions } from "@/components/lookups";
import { Empty, ErrorState, Loading, PageHeader, Section } from "@/components/page";
import { daysAgo, humanize, isoDate } from "@/lib/format";
import { useParamState } from "@/lib/params";

const reports = [
  { value: "supply", label: "Supply: applicants, vetting and active providers" },
  { value: "demand", label: "Demand: requests by category and area" },
  { value: "matching", label: "Matching: time to assign and fill rate" },
  { value: "quality", label: "Quality: ratings, complaints and spot checks" },
  { value: "commercial", label: "Commercial: revenue, commission and payouts" },
  { value: "compliance", label: "Compliance: documents, expiries and vault access" },
] as const;

type ReportName = (typeof reports)[number]["value"];

function fmt(v: unknown): string {
  if (typeof v === "number") return Number.isInteger(v) ? v.toLocaleString() : v.toFixed(2);
  if (v === null || v === undefined) return "—";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

/** The monthly management pack (report §10). Each report can be exported as CSV. */
export function ReportsPage() {
  const [f, set] = useParamState(["report", "from", "to", "group_by", "category_id", "service_area_id"]);
  const report = (f.report ?? "supply") as ReportName;
  const from = f.from ?? daysAgo(30);
  const to = f.to ?? isoDate(new Date());
  const categories = useCategoryOptions();
  const areas = useAreaOptions();
  const [exporting, setExporting] = useState(false);
  const { data, isLoading, error } = $api.useQuery("get", "/console/reports/{report}", {
    params: {
      path: { report },
      query: {
        from,
        to,
        group_by: f.group_by as "day" | "week" | "month" | "category" | "service_area" | undefined,
        category_id: f.category_id,
        service_area_id: f.service_area_id,
      },
    },
  });

  async function exportCsv() {
    setExporting(true);
    try {
      await download(`/console/reports/${report}/export`, { from, to }, `${report}-${from}-to-${to}.csv`);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setExporting(false);
    }
  }

  const metrics = Object.entries((data?.metrics ?? {}) as Record<string, unknown>);
  const series = data?.series ?? [];
  const seriesKeys = [...new Set(series.flatMap((s) => Object.keys((s.values ?? {}) as object)))];

  return (
    <>
      <PageHeader
        title="Reports"
        description="Monthly management pack metrics."
        actions={
          <Button variant="outline" size="sm" onClick={exportCsv} disabled={exporting}>
            <Download /> {exporting ? "Exporting…" : "Export CSV"}
          </Button>
        }
      />
      <FilterBar>
        <EnumFilter label="Report" value={report} options={[...reports]} allLabel="Supply" onChange={(v) => set("report", v)} />
        <TextFilter label="From" type="date" value={from} onChange={(v) => set("from", v)} />
        <TextFilter label="To" type="date" value={to} onChange={(v) => set("to", v)} />
        <EnumFilter label="Group by" value={f.group_by} options={["day", "week", "month", "category", "service_area"]} allLabel="None" onChange={(v) => set("group_by", v)} />
        <EnumFilter label="Category" value={f.category_id} options={categories} onChange={(v) => set("category_id", v)} />
        <EnumFilter label="Area" value={f.service_area_id} options={areas} onChange={(v) => set("service_area_id", v)} />
      </FilterBar>
      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            {metrics.length ? (
              metrics.map(([k, v]) => (
                <Card key={k} className="gap-1 px-4 py-3">
                  <div className="text-muted-foreground text-xs">{humanize(k)}</div>
                  <div className="text-lg font-semibold tabular-nums">{fmt(v)}</div>
                </Card>
              ))
            ) : (
              <Empty>No metrics for this period.</Empty>
            )}
          </div>
          {series.length > 0 && (
            <Section className="mt-4" title={`By ${humanize(data?.group_by ?? "period").toLowerCase()}`}>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{humanize(data?.group_by ?? "bucket")}</TableHead>
                    {seriesKeys.map((k) => (
                      <TableHead key={k} className="text-right">
                        {humanize(k)}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {series.map((s, i) => (
                    <TableRow key={s.bucket ?? i}>
                      <TableCell>{s.bucket}</TableCell>
                      {seriesKeys.map((k) => (
                        <TableCell key={k} className="text-right tabular-nums">
                          {fmt((s.values as Record<string, unknown> | undefined)?.[k])}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Section>
          )}
        </>
      )}
    </>
  );
}
