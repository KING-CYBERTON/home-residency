import { useState, type ReactNode } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { errorMessage } from "@/api/client";
import { humanize } from "@/lib/format";

type Option = { value: string; label: string };
type Values = Record<string, unknown>;

export type Field = {
  name: string;
  label: string;
  type?:
    | "text"
    | "textarea"
    | "number"
    | "money" // entered in KES, sent as minor units
    | "date"
    | "datetime"
    | "time" // HH:MM; an empty field is sent as "" so an edit can clear it
    | "select"
    | "multiselect"
    | "checkbox"
    | "lines" // one entry per line -> string[]
    | "json";
  options?: readonly string[] | Option[];
  required?: boolean;
  placeholder?: string;
  help?: ReactNode;
  show?: (values: Values) => boolean;
};

function toOptions(options: Field["options"]): Option[] {
  return (options ?? []).map((o) => (typeof o === "string" ? { value: o, label: humanize(o) } : o));
}

function toLocalInput(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Turns the form's string state into the JSON body; empty optional fields are left out. */
function toBody(fields: Field[], values: Values): Values {
  const out: Values = {};
  for (const f of fields) {
    if (f.show && !f.show(values)) continue;
    const v = values[f.name];
    switch (f.type) {
      case "checkbox":
        if (v !== undefined) out[f.name] = Boolean(v);
        break;
      case "multiselect":
        if (Array.isArray(v) && (v.length || f.required)) out[f.name] = v;
        break;
      case "number":
        if (v !== "" && v !== undefined) out[f.name] = Number(v);
        break;
      case "money":
        if (v !== "" && v !== undefined) out[f.name] = Math.round(Number(v) * 100);
        break;
      case "datetime":
        if (v) out[f.name] = new Date(String(v)).toISOString();
        break;
      case "time":
        if (v !== undefined) out[f.name] = String(v);
        break;
      case "lines":
        if (typeof v === "string" && v.trim())
          out[f.name] = v
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean);
        break;
      case "json":
        if (typeof v === "string" && v.trim()) out[f.name] = JSON.parse(v);
        break;
      default:
        if (v !== "" && v !== undefined) out[f.name] = v;
    }
  }
  return out;
}

/** Converts an existing record into form state, the reverse of toBody. */
function toState(fields: Field[], initial: Values): Values {
  const state: Values = {};
  for (const f of fields) {
    const v = initial[f.name];
    if (v === undefined || v === null) {
      state[f.name] = f.type === "multiselect" ? [] : f.type === "checkbox" ? false : "";
      continue;
    }
    switch (f.type) {
      case "money":
        state[f.name] = String(Number(v) / 100);
        break;
      case "datetime":
        state[f.name] = toLocalInput(String(v));
        break;
      case "lines":
        state[f.name] = Array.isArray(v) ? v.join("\n") : String(v);
        break;
      case "json":
        state[f.name] = JSON.stringify(v, null, 2);
        break;
      case "date":
        state[f.name] = String(v).slice(0, 10);
        break;
      default:
        state[f.name] = v;
    }
  }
  return state;
}

export function FieldInput({
  field,
  value,
  onChange,
}: {
  field: Field;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const id = `f-${field.name}`;
  const common = { id, required: field.required, placeholder: field.placeholder };
  switch (field.type) {
    case "textarea":
    case "lines":
    case "json":
      return (
        <Textarea
          {...common}
          rows={field.type === "json" ? 6 : 4}
          className={field.type === "json" ? "font-mono text-xs" : undefined}
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    case "select":
      return (
        <NativeSelect {...common} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)}>
          <option value="">{field.required ? "Choose…" : "None"}</option>
          {toOptions(field.options).map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </NativeSelect>
      );
    case "multiselect": {
      const selected = (value as string[]) ?? [];
      return (
        <div className="grid max-h-48 grid-cols-1 gap-2 overflow-y-auto rounded-md border p-3 sm:grid-cols-2">
          {toOptions(field.options).map((o) => (
            <label key={o.value} className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={selected.includes(o.value)}
                onCheckedChange={(c) =>
                  onChange(c ? [...selected, o.value] : selected.filter((s) => s !== o.value))
                }
              />
              {o.label}
            </label>
          ))}
        </div>
      );
    }
    case "checkbox":
      return (
        <label className="flex items-center gap-2 text-sm">
          <Checkbox id={id} checked={Boolean(value)} onCheckedChange={(c) => onChange(Boolean(c))} />
          {field.label}
        </label>
      );
    case "number":
    case "money":
      return (
        <Input
          {...common}
          type="number"
          step={field.type === "money" ? "0.01" : "any"}
          value={String(value ?? "")}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    case "date":
      return <Input {...common} type="date" value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />;
    case "datetime":
      return (
        <Input {...common} type="datetime-local" value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />
      );
    case "time":
      return <Input {...common} type="time" step={900} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />;
    default:
      return <Input {...common} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />;
  }
}

/**
 * A button that opens a dialog with a small form and calls the API on submit.
 * Most console actions (claim, assign, decide, suspend…) are one of these.
 */
export function FormDialog({
  trigger,
  triggerVariant = "outline",
  triggerSize = "sm",
  title,
  description,
  fields = [],
  initial = {},
  submitLabel,
  destructive,
  success,
  onSubmit,
  onDone,
  disabled,
  autoOpen,
  children,
}: {
  trigger: ReactNode;
  triggerVariant?: "default" | "outline" | "secondary" | "destructive" | "ghost";
  triggerSize?: "default" | "sm";
  title: string;
  description?: ReactNode;
  fields?: Field[];
  initial?: Values;
  submitLabel?: string;
  destructive?: boolean;
  success?: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  onSubmit: (body: any) => Promise<unknown>;
  onDone?: (result: unknown) => void;
  disabled?: boolean;
  /** Open on first render, e.g. when another page links here to start this action. */
  autoOpen?: boolean;
  children?: ReactNode;
}) {
  const [open, setOpen] = useState(Boolean(autoOpen));
  const [values, setValues] = useState<Values>(() => (autoOpen ? toState(fields, initial) : {}));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function openChange(next: boolean) {
    if (next) {
      setValues(toState(fields, initial));
      setError(null);
    }
    setOpen(next);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const result = await onSubmit(toBody(fields, values));
      toast.success(success ?? title);
      setOpen(false);
      onDone?.(result);
    } catch (err) {
      setError(err instanceof SyntaxError ? `Invalid JSON: ${err.message}` : errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={openChange}>
      <DialogTrigger asChild>
        <Button variant={triggerVariant} size={triggerSize} disabled={disabled}>
          {trigger}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={submit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            {description && <DialogDescription>{description}</DialogDescription>}
          </DialogHeader>
          {children}
          {fields
            .filter((f) => !f.show || f.show(values))
            .map((f) => (
              <div key={f.name} className="grid gap-1.5">
                {f.type !== "checkbox" && (
                  <Label htmlFor={`f-${f.name}`}>
                    {f.label}
                    {!f.required && <span className="text-muted-foreground font-normal">(optional)</span>}
                  </Label>
                )}
                <FieldInput field={f} value={values[f.name]} onChange={(v) => setValues((s) => ({ ...s, [f.name]: v }))} />
                {f.help && <p className="text-muted-foreground text-xs">{f.help}</p>}
              </div>
            ))}
          {error && (
            <p role="alert" className="border-destructive/30 bg-destructive/5 text-destructive rounded-md border p-2 text-sm">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" variant={destructive ? "destructive" : "default"} disabled={busy}>
              {busy ? "Working…" : (submitLabel ?? title)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Shows the action only to roles allowed to call it. */
export function Allowed({ when, children }: { when: boolean; children: ReactNode }) {
  return when ? <>{children}</> : null;
}
