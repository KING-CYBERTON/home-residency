import type { ReactNode } from "react";

import type { Schemas } from "@/api/client";
import { DataTable } from "@/components/data-table";
import { browserName } from "@/lib/browser";
import { ago, dateTime } from "@/lib/format";

/** Where someone is signed in. end, when given, adds a button to end one session. */
export function SessionsTable({
  rows,
  loading,
  error,
  end,
}: {
  rows: Schemas["SessionInfo"][] | undefined;
  loading?: boolean;
  error?: unknown;
  end?: (s: Schemas["SessionInfo"]) => ReactNode;
}) {
  return (
    <DataTable<Schemas["SessionInfo"]>
      rows={rows}
      loading={loading}
      error={error}
      rowKey={(s) => s.id}
      empty="Not signed in anywhere."
      columns={[
        {
          header: "Browser",
          cell: (s) => (
            <span title={s.user_agent ?? undefined}>
              {browserName(s.user_agent)}
              {s.current && <span className="text-muted-foreground text-xs"> (this browser)</span>}
            </span>
          ),
        },
        { header: "IP address", cell: (s) => <span className="font-mono text-xs">{s.ip_address ?? "—"}</span> },
        { header: "Signed in", cell: (s) => dateTime(s.created_at) },
        { header: "Last active", cell: (s) => ago(s.last_seen_at) },
        { header: "Authenticator", cell: (s) => (s.mfa_passed ? "Checked" : "Not yet") },
        ...(end ? [{ header: "", cell: (s: Schemas["SessionInfo"]) => <div className="flex justify-end">{end(s)}</div> }] : []),
      ]}
    />
  );
}
