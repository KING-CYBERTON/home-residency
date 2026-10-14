import type { ReactNode } from "react";

import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { humanize } from "@/lib/format";

export function FilterBar({ children }: { children: ReactNode }) {
  return <div className="mb-4 flex flex-wrap items-end gap-3">{children}</div>;
}

export function FilterField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex min-w-36 flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

export function EnumFilter({
  label,
  value,
  options,
  onChange,
  allLabel = "All",
}: {
  label: string;
  value: string | undefined;
  options: readonly string[] | { value: string; label: string }[];
  onChange: (v: string | undefined) => void;
  allLabel?: string;
}) {
  const opts = options.map((o) => (typeof o === "string" ? { value: o, label: humanize(o) } : o));
  return (
    <FilterField label={label}>
      <NativeSelect value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
        <option value="">{allLabel}</option>
        {opts.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </NativeSelect>
    </FilterField>
  );
}

export function TextFilter({
  label,
  value,
  onChange,
  type = "text",
  placeholder,
}: {
  label: string;
  value: string | undefined;
  onChange: (v: string | undefined) => void;
  type?: "text" | "date" | "number";
  placeholder?: string;
}) {
  return (
    <FilterField label={label}>
      <Input
        type={type}
        value={value ?? ""}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value || undefined)}
      />
    </FilterField>
  );
}
