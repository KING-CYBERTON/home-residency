import { useMemo } from "react";

import { operationInfo, operationRoles } from "@/api/roles.gen";
import { StaffRole } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar, TextFilter } from "@/components/filters";
import { PageHeader } from "@/components/page";
import { Badge } from "@/components/ui/badge";
import { canCall, roleLabels, type StaffRole as Role } from "@/lib/access";
import { useParamState } from "@/lib/params";

type Row = { op: string; summary: string; area: string; roles: Role[]; anyone: boolean };

const roleOptions = StaffRole.map((r) => ({ value: r, label: roleLabels[r] }));

/** Read straight from the API contract, so it always matches what the server enforces. */
const rows: Row[] = Object.entries(operationRoles).map(([op, allowed]) => {
  const anyone = allowed.includes("any staff") || (op.startsWith("GET ") && allowed.includes("any staff (read)"));
  return {
    op,
    summary: operationInfo[op]?.summary || op,
    area: (operationInfo[op]?.area ?? "").replace(/^Console: /, ""),
    roles: StaffRole.filter((r) => r !== "admin" && allowed.includes(r)),
    anyone,
  };
});
const areas = [...new Set(rows.map((r) => r.area))].sort();

export function RolesPage() {
  const [f, set] = useParamState(["role", "area", "q"]);
  const shown = useMemo(() => {
    const q = (f.q ?? "").toLowerCase();
    return rows.filter(
      (r) =>
        (!f.role || canCall([f.role], r.op)) &&
        (!f.area || r.area === f.area) &&
        (!q || r.summary.toLowerCase().includes(q) || r.op.toLowerCase().includes(q)),
    );
  }, [f.role, f.area, f.q]);
  return (
    <>
      <PageHeader
        title="Roles"
        description="What each role can do in the console. Admin holds every role. Two-person rules still apply on top of roles: whoever prepares or investigates something cannot also approve or decide it. Change someone's roles on the Staff page."
      />
      <FilterBar>
        <EnumFilter label="Role" value={f.role} options={roleOptions} onChange={(v) => set("role", v)} allLabel="Every role" />
        <EnumFilter label="Area" value={f.area} options={areas.map((a) => ({ value: a, label: a }))} onChange={(v) => set("area", v)} />
        <TextFilter label="Search" value={f.q} onChange={(v) => set("q", v)} placeholder="e.g. refund" />
      </FilterBar>
      <p className="text-muted-foreground mb-2 text-xs">
        {shown.length} of {rows.length} actions
        {f.role === "admin" ? " (admin can do all of them)" : ""}
      </p>
      <DataTable<Row>
        rows={shown}
        rowKey={(r) => r.op}
        empty="No actions match."
        columns={[
          { header: "Area", cell: (r) => <span className="text-xs">{r.area}</span> },
          {
            header: "Action",
            cell: (r) => (
              <div>
                <div>{r.summary}</div>
                <code className="text-muted-foreground text-[11px]">{r.op}</code>
              </div>
            ),
          },
          {
            header: "Who can",
            cell: (r) =>
              r.anyone ? (
                <Badge variant="secondary">Any staff</Badge>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {r.roles.map((role) => (
                    <Badge key={role} variant="outline">
                      {roleLabels[role]}
                    </Badge>
                  ))}
                  <Badge variant="secondary">Admin</Badge>
                </div>
              ),
          },
        ]}
      />
    </>
  );
}
