import { $api, type Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { Allowed, FormDialog, type Field } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { useCan } from "@/lib/access";
import { dateTime } from "@/lib/format";

const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const appraisalParts = [
  ["rating", "Client ratings"],
  ["complaints", "Complaints"],
  ["reliability", "Reliability"],
  ["spot_checks", "Spot checks"],
] as const;

type Kind = "number" | "integer" | "text" | "weights" | "payout_cycle";

/**
 * How each known setting is named, shown and edited. The API checks the same types and ranges.
 * A setting missing here is shown as JSON, so a new one can still be changed before the console knows it.
 */
const known: Record<string, { label: string; kind: Kind; unit?: string; min?: number; max?: number }> = {
  "appraisal.weights": { label: "Appraisal weights", kind: "weights" },
  "checkin.max_distance_m": { label: "Check-in distance", kind: "integer", unit: "metres", min: 25, max: 5000 },
  "commission.default_pct": { label: "Default commission", kind: "number", unit: "%", min: 0, max: 100 },
  "contract.code_of_conduct_version": { label: "Code of conduct version", kind: "text" },
  "contract.current_version": { label: "Provider contract version", kind: "text" },
  "document.expiry_warning_days": { label: "Document expiry warning", kind: "integer", unit: "days", min: 1, max: 365 },
  "offer.expiry_minutes": { label: "Offer expiry", kind: "integer", unit: "minutes", min: 5, max: 1440 },
  "otp.max_attempts": { label: "Code attempts", kind: "integer", unit: "tries", min: 1, max: 20 },
  "otp.ttl_minutes": { label: "Code lifetime", kind: "integer", unit: "minutes", min: 1, max: 60 },
  "payout.cycle": { label: "Payout day", kind: "payout_cycle" },
  "rating.complaint_threshold": { label: "Complaint from a rating", kind: "integer", unit: "stars or fewer", min: 1, max: 5 },
  "session.client_ttl_hours": { label: "Client and provider sign-in", kind: "integer", unit: "hours", min: 1, max: 8760 },
  "session.staff_ttl_hours": { label: "Staff sign-in", kind: "integer", unit: "hours", min: 1, max: 24 },
  "vat.rate_pct": { label: "VAT rate", kind: "number", unit: "%", min: 0, max: 50 },
};

function show(s: Schemas["Setting"]): string {
  const k = known[s.key];
  const v = s.value as unknown;
  if (!k) return JSON.stringify(v);
  switch (k.kind) {
    case "weights": {
      const w = (v ?? {}) as Record<string, number>;
      return appraisalParts.map(([key, label]) => `${label} ${Math.round((w[key] ?? 0) * 100)}%`).join(", ");
    }
    case "payout_cycle": {
      const c = (v ?? {}) as { weekday?: number };
      return `Weekly on ${weekdays[c.weekday ?? 5] ?? "—"}`;
    }
    case "text":
      return String(v);
    default:
      return k.unit === "%" ? `${String(v)}%` : `${String(v)} ${k.unit ?? ""}`.trim();
  }
}

/** The form fields for a setting and how they turn back into its value. */
function editor(s: Schemas["Setting"]): { fields: Field[]; initial: Record<string, unknown>; value: (b: Record<string, unknown>) => unknown } {
  const k = known[s.key];
  const v = s.value as unknown;
  if (!k) {
    return {
      fields: [{ name: "value", label: "Value (JSON)", type: "json", required: true, help: 'Numbers as 4.5, text in quotes "like this", objects as {"a": 1}.' }],
      initial: { value: v },
      value: (b) => b.value,
    };
  }
  switch (k.kind) {
    case "weights": {
      const w = (v ?? {}) as Record<string, number>;
      return {
        fields: appraisalParts.map(([key, label]) => ({ name: key, label: `${label} (%)`, type: "number" as const, required: true })),
        initial: Object.fromEntries(appraisalParts.map(([key]) => [key, Math.round((w[key] ?? 0) * 1000) / 10])),
        value: (b) => Object.fromEntries(appraisalParts.map(([key]) => [key, Math.round(Number(b[key]) * 10) / 1000])),
      };
    }
    case "payout_cycle": {
      const c = (v ?? {}) as { weekday?: number };
      return {
        fields: [{ name: "weekday", label: "Pay providers every", type: "select", required: true, options: weekdays.map((d, i) => ({ value: String(i), label: d })) }],
        initial: { weekday: String(c.weekday ?? 5) },
        value: (b) => ({ frequency: "weekly", weekday: Number(b.weekday) }),
      };
    }
    case "text":
      return { fields: [{ name: "value", label: k.label, required: true }], initial: { value: v }, value: (b) => b.value };
    default: {
      const range = k.min !== undefined && k.max !== undefined ? `Between ${k.min} and ${k.max}${k.unit === "%" ? "%" : ` ${k.unit}`}.` : undefined;
      return {
        fields: [{ name: "value", label: k.unit ? `${k.label} (${k.unit})` : k.label, type: "number", required: true, help: range }],
        initial: { value: v },
        value: (b) => b.value,
      };
    }
  }
}

export function SettingsPage() {
  const { data, isLoading, error } = $api.useQuery("get", "/console/settings", {});
  const put = $api.useMutation("put", "/console/settings/{key}");
  const canEdit = useCan("PUT /console/settings/{key}");
  return (
    <>
      <PageHeader
        title="Settings"
        description="Tunable business rules: rating threshold, offer expiry, check-in radius, appraisal weights, payout day and more. Every change is audited."
      />
      <DataTable<Schemas["Setting"]>
        rows={data}
        loading={isLoading}
        error={error}
        rowKey={(s) => s.key}
        empty="No settings."
        columns={[
          {
            header: "Setting",
            cell: (s) => (
              <div>
                <div>{known[s.key]?.label ?? s.key}</div>
                <code className="text-muted-foreground text-xs">{s.key}</code>
              </div>
            ),
          },
          { header: "Value", cell: (s) => <span className="font-medium">{show(s)}</span> },
          { header: "What it does", cell: (s) => <span className="text-muted-foreground text-xs">{s.description}</span> },
          { header: "Changed", cell: (s) => dateTime(s.updated_at) },
          {
            header: "",
            cell: (s) => {
              const e = editor(s);
              return (
                <Allowed when={canEdit}>
                  <FormDialog
                    trigger="Change"
                    title={`Change ${known[s.key]?.label.toLowerCase() ?? s.key}`}
                    description={s.description ?? undefined}
                    fields={e.fields}
                    initial={e.initial}
                    submitLabel="Save"
                    onSubmit={(body) => put.mutateAsync({ params: { path: { key: s.key } }, body: { value: e.value(body) } })}
                  />
                </Allowed>
              );
            },
          },
        ]}
      />
    </>
  );
}
