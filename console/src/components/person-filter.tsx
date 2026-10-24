import { XIcon } from "lucide-react";
import { useState } from "react";

import { $api } from "@/api/client";
import { FilterField } from "@/components/filters";
import { Input } from "@/components/ui/input";
import { shortId } from "@/lib/format";

export type PersonKind = "client" | "provider" | "staff";

type Person = { id: string; full_name: string; hint?: string | null };

const noun: Record<PersonKind, string> = { client: "client", provider: "provider", staff: "staff member" };

/** The people of one kind whose name or phone matches the search. Staff are few, so they are matched here. */
function useMatches(kind: PersonKind, q: string): Person[] {
  const on = q.trim().length >= 2;
  const query = { params: { query: { q: q.trim(), limit: 8 } } };
  const clients = $api.useQuery("get", "/console/clients", query, { enabled: on && kind === "client" });
  const providers = $api.useQuery("get", "/console/providers", query, { enabled: on && kind === "provider" });
  const staff = $api.useQuery("get", "/console/staff", {}, { enabled: on && kind === "staff", staleTime: 5 * 60_000 });
  if (!on) return [];
  if (kind === "client") return (clients.data?.data ?? []).map((c) => ({ id: c.id, full_name: c.full_name, hint: c.phone }));
  if (kind === "provider") return (providers.data?.data ?? []).map((p) => ({ id: p.id, full_name: p.full_name, hint: p.phone }));
  const needle = q.trim().toLowerCase();
  return (staff.data ?? [])
    .filter((s) => s.full_name.toLowerCase().includes(needle) || s.email.toLowerCase().includes(needle))
    .slice(0, 8)
    .map((s) => ({ id: s.id, full_name: s.full_name, hint: s.email }));
}

/** The name of the person a filter already holds, such as one that came in a link. */
function usePersonName(kind: PersonKind, id: string | undefined): string | undefined {
  const path = { params: { path: { id: id ?? "" } } };
  const client = $api.useQuery("get", "/console/clients/{id}", path, { enabled: !!id && kind === "client", retry: false });
  const provider = $api.useQuery("get", "/console/providers/{id}", path, { enabled: !!id && kind === "provider", retry: false });
  const staff = $api.useQuery("get", "/console/staff", {}, { enabled: !!id && kind === "staff", staleTime: 5 * 60_000 });
  if (!id) return undefined;
  if (kind === "client") return client.data?.full_name;
  if (kind === "provider") return provider.data?.full_name;
  return staff.data?.find((s) => s.id === id)?.full_name;
}

/** Finds a person by name or phone and keeps their id, in place of a raw ID box. */
export function PersonPicker({
  kind,
  value,
  onChange,
  label,
  id,
  required,
}: {
  kind: PersonKind;
  value: string | undefined;
  onChange: (id: string | undefined) => void;
  label: string;
  id?: string;
  required?: boolean;
}) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const matches = useMatches(kind, q);
  const name = usePersonName(kind, value);

  if (value) {
    return (
      <span className="border-input flex h-9 items-center gap-2 rounded-md border px-3 text-sm">
        <span className="truncate">{name ?? shortId(value)}</span>
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground ml-auto"
          aria-label={`Clear ${label.toLowerCase()}`}
          onClick={() => onChange(undefined)}
        >
          <XIcon className="size-4" />
        </button>
      </span>
    );
  }

  const pick = (p: Person) => {
    onChange(p.id);
    setQ("");
    setOpen(false);
  };
  return (
    <div className="relative">
      <Input
        id={id}
        role="combobox"
        aria-expanded={open && matches.length > 0}
        aria-label={label}
        required={required}
        value={q}
        placeholder={`Search ${noun[kind]} by name or phone`}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            if (matches[0]) pick(matches[0]);
          }
          if (e.key === "Escape") setOpen(false);
        }}
      />
      {open && q.trim().length >= 2 && (
        <ul role="listbox" className="bg-popover absolute z-50 mt-1 max-h-64 w-72 overflow-auto rounded-md border p-1 shadow-md">
          {matches.length === 0 && <li className="text-muted-foreground px-2 py-1.5 text-sm">No {noun[kind]} matches</li>}
          {matches.map((p) => (
            <li
              key={p.id}
              role="option"
              aria-selected={false}
              className="hover:bg-accent cursor-pointer rounded px-2 py-1.5 text-sm"
              onMouseDown={(e) => {
                e.preventDefault();
                pick(p);
              }}
            >
              <div>{p.full_name}</div>
              {p.hint && <div className="text-muted-foreground text-xs">{p.hint}</div>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function PersonFilter(props: { label: string; kind: PersonKind; value: string | undefined; onChange: (id: string | undefined) => void }) {
  return (
    <FilterField label={props.label}>
      <PersonPicker {...props} />
    </FilterField>
  );
}
