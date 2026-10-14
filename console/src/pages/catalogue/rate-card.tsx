import { $api, type Schemas } from "@/api/client";
import { Checkbox } from "@/components/ui/checkbox";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useAreaName, useAreaOptions, useServiceName, useServiceOptions } from "@/components/lookups";
import { PageHeader } from "@/components/page";
import { useCan } from "@/lib/access";
import { dateTime, money } from "@/lib/format";
import { useParamState } from "@/lib/params";

export function RateCardPage() {
  const [f, set] = useParamState(["service_id", "service_area_id", "all"]);
  const services = useServiceOptions();
  const areas = useAreaOptions();
  const serviceName = useServiceName();
  const areaName = useAreaName();
  const publish = $api.useMutation("post", "/console/rate-card");
  const canPublish = useCan("POST /console/rate-card");
  const { data, isLoading, error } = $api.useQuery("get", "/console/rate-card", {
    params: { query: { service_id: f.service_id, service_area_id: f.service_area_id, current_only: f.all !== "true" } },
  });
  return (
    <>
      <PageHeader
        title="Rate card"
        description="Prices are versioned: publishing a new price closes the current one, and validity windows never overlap. Commission is the client price less provider pay."
        actions={
          <Allowed when={canPublish}>
            <FormDialog
              trigger="Publish price"
              triggerVariant="default"
              title="Publish a new price"
              fields={[
                { name: "service_id", label: "Service", type: "select", options: services, required: true },
                { name: "service_area_id", label: "Area", type: "select", options: areas, help: "Leave empty for every area." },
                { name: "client_price_minor", label: "Client price (KES, incl. VAT)", type: "money", required: true },
                { name: "provider_pay_minor", label: "Provider pay (KES)", type: "money", required: true },
                { name: "placement_fee_minor", label: "Placement fee (KES)", type: "money" },
                { name: "valid_from", label: "Valid from", type: "datetime", required: true },
                { name: "valid_to", label: "Valid to", type: "datetime" },
              ]}
              initial={{ service_id: f.service_id, service_area_id: f.service_area_id, valid_from: new Date().toISOString() }}
              submitLabel="Publish"
              onSubmit={(body) => publish.mutateAsync({ body })}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Service" value={f.service_id} options={services} onChange={(v) => set("service_id", v)} />
        <EnumFilter label="Area" value={f.service_area_id} options={areas} onChange={(v) => set("service_area_id", v)} />
        <label className="flex h-9 items-center gap-2 text-sm">
          <Checkbox checked={f.all === "true"} onCheckedChange={(c) => set("all", c ? "true" : undefined)} />
          Include past prices
        </label>
      </FilterBar>
      <DataTable<Schemas["RateCardEntry"]>
        rows={data}
        loading={isLoading}
        error={error}
        rowKey={(r) => r.id}
        empty="No prices published."
        columns={[
          { header: "Service", cell: (r) => serviceName(r.service_id) },
          { header: "Area", cell: (r) => (r.service_area_id ? areaName(r.service_area_id) : "All areas") },
          { header: "Client price", cell: (r) => money(r.client_price_minor), className: "text-right tabular-nums" },
          { header: "Provider pay", cell: (r) => money(r.provider_pay_minor), className: "text-right tabular-nums" },
          {
            header: "Commission",
            cell: (r) => (r.provider_pay_minor != null ? money(r.client_price_minor - r.provider_pay_minor) : "—"),
            className: "text-right tabular-nums",
          },
          { header: "Placement fee", cell: (r) => money(r.placement_fee_minor), className: "text-right tabular-nums" },
          { header: "Valid from", cell: (r) => dateTime(r.valid_from) },
          { header: "Valid to", cell: (r) => (r.valid_to ? dateTime(r.valid_to) : "Open") },
        ]}
      />
    </>
  );
}
