import type { Schemas } from "@/api/client";
import { DocumentType } from "@/api/enums.gen";

// The checklist editor works on rows of plain strings (what the inputs hold) and turns them into
// API items on save. Everything here is pure, so the editor, the list page and the diff share it.

export type ChecklistItem = Schemas["ChecklistTemplateItem"];
export type ChecklistTemplate = Schemas["ChecklistTemplate"];
export type Kind = ChecklistItem["kind"];
type DocType = (typeof DocumentType)[number];

/** Requirement types in the order the editor offers them, with the words staff use. */
export const kinds: { value: Kind; label: string }[] = [
  { value: "document", label: "Document upload" },
  { value: "clearance", label: "Background check (Good Conduct)" },
  { value: "referee", label: "Referees" },
  { value: "interview", label: "Interview" },
  { value: "practical_assessment", label: "Practical / skills test" },
  { value: "medical", label: "Medical" },
  { value: "training", label: "Training" },
  { value: "trade_licence", label: "Trade licence" },
];
export const kindLabel = (k: string) =>
  kinds.find((x) => x.value === k)?.label ?? k;

export const documentLabels: Record<DocType, string> = {
  national_id: "National ID",
  passport: "Passport",
  kra_pin: "KRA PIN certificate",
  good_conduct_certificate: "Certificate of Good Conduct",
  nssf_card: "NSSF card",
  shif_card: "SHIF card",
  academic_certificate: "Academic certificate",
  trade_certificate: "Trade certificate",
  trade_licence: "Trade licence",
  medical_certificate: "Medical certificate",
  first_aid_certificate: "First aid certificate",
  reference_letter: "Reference letter",
  passport_photo: "Passport photo",
  signed_contract: "Signed contract",
  other: "Other document",
};
export const documentOptions = DocumentType.map((d) => ({
  value: d,
  label: documentLabels[d],
}));
export const documentLabel = (d: string | null | undefined) =>
  d ? (documentLabels[d as DocType] ?? d) : "";

/** Which document is asked for only on a document upload; these kinds can lapse and be checked again. */
export const needsDocument = (k: string) => k === "document";
export const canExpire = (k: string) =>
  ["document", "clearance", "trade_licence", "medical", "training"].includes(k);
export const hasCount = (k: string) => k === "referee";

export type Row = {
  key: string;
  kind: Kind;
  label: string;
  required: boolean;
  documentType: string;
  minCount: string;
  validityMonths: string;
};

let seq = 0;
const newKey = () => `r${++seq}`;

export function blankRow(): Row {
  return {
    key: newKey(),
    kind: "document",
    label: "",
    required: true,
    documentType: "",
    minCount: "2",
    validityMonths: "",
  };
}

export function rowFromItem(i: ChecklistItem): Row {
  return {
    key: newKey(),
    kind: i.kind,
    label: i.label,
    required: i.is_mandatory,
    documentType: i.document_type ?? "",
    minCount: String(i.min_count ?? (i.kind === "referee" ? 2 : 1)),
    validityMonths: i.validity_months ? String(i.validity_months) : "",
  };
}

/** The API item for a row: fields that do not apply to its type are left out. */
export function itemFromRow(r: Row, n: number): ChecklistItem {
  const months =
    canExpire(r.kind) && r.validityMonths.trim()
      ? Number(r.validityMonths)
      : undefined;
  return {
    kind: r.kind,
    label: r.label.trim(),
    is_mandatory: r.required,
    document_type:
      needsDocument(r.kind) && r.documentType
        ? (r.documentType as DocType)
        : undefined,
    min_count: hasCount(r.kind) ? Number(r.minCount) : 1,
    validity_months: months,
    sort_order: n + 1,
  };
}

// ---- the Standard base: what every provider is asked for, whatever the category

type StandardItem = ChecklistItem & { std: string };
export const standard: StandardItem[] = [
  {
    std: "national_id",
    kind: "document",
    document_type: "national_id",
    label: "National ID (both sides)",
    is_mandatory: true,
  },
  {
    std: "passport_photo",
    kind: "document",
    document_type: "passport_photo",
    label: "Passport photo",
    is_mandatory: true,
  },
  {
    std: "kra_pin",
    kind: "document",
    document_type: "kra_pin",
    label: "KRA PIN certificate",
    is_mandatory: true,
  },
  {
    std: "good_conduct",
    kind: "clearance",
    label: "Certificate of Good Conduct (DCI)",
    is_mandatory: true,
    validity_months: 12,
  },
  {
    std: "referees",
    kind: "referee",
    label: "Referees",
    is_mandatory: true,
    min_count: 2,
  },
  {
    std: "interview",
    kind: "interview",
    label: "In-person interview",
    is_mandatory: true,
  },
  {
    std: "induction",
    kind: "training",
    label: "Homend induction training",
    is_mandatory: true,
  },
];

/** The Standard requirement an item stands for, matched by what it is rather than its exact wording. */
export function standardKey(i: {
  kind: string;
  document_type?: string | null;
  label: string;
}): string | undefined {
  if (i.kind === "training")
    return /induction/i.test(i.label) ? "induction" : undefined;
  return standard.find(
    (s) =>
      s.kind === i.kind &&
      (s.kind !== "document" || s.document_type === i.document_type),
  )?.std;
}
export const standardLabel = (std: string) =>
  standard.find((s) => s.std === std)?.label ?? std;

/** How a checklist differs from Standard: Standard items it lacks, and the extras it adds. */
export function vsStandard(items: ChecklistItem[]) {
  const keys = new Set(items.map(standardKey).filter(Boolean));
  return {
    missing: standard.filter((s) => !keys.has(s.std)).map((s) => s.label),
    extra: items.filter((i) => !standardKey(i)).map((i) => i.label),
  };
}

// ---- validation, shown on each row as it is edited

export function rowProblems(rows: Row[]): Record<string, string[]> {
  const out: Record<string, string[]> = {};
  const names = new Map<string, number>();
  for (const r of rows)
    names.set(
      r.label.trim().toLowerCase(),
      (names.get(r.label.trim().toLowerCase()) ?? 0) + 1,
    );
  for (const r of rows) {
    const p: string[] = [];
    const name = r.label.trim();
    if (!name) p.push("Give it a name.");
    else if ((names.get(name.toLowerCase()) ?? 0) > 1)
      p.push("Another requirement has this name.");
    if (needsDocument(r.kind) && !r.documentType)
      p.push("Choose which document.");
    if (canExpire(r.kind) && r.validityMonths.trim()) {
      const m = Number(r.validityMonths);
      if (!Number.isInteger(m) || m < 1 || m > 120)
        p.push("Expiry must be a whole number of months, 1 to 120.");
    }
    if (hasCount(r.kind)) {
      const c = Number(r.minCount);
      if (!Number.isInteger(c) || c < 1 || c > 10)
        p.push("Ask for 1 to 10 referees.");
    }
    if (p.length) out[r.key] = p;
  }
  return out;
}

// ---- diff against the current version

export type DiffLine =
  | { change: "same" | "added"; item: ChecklistItem }
  | {
      change: "changed";
      item: ChecklistItem;
      was: ChecklistItem;
      notes: string[];
    }
  | { change: "removed"; item: ChecklistItem };

const sameThing = (a: ChecklistItem, b: ChecklistItem) =>
  a.kind === b.kind &&
  (a.kind !== "document" ||
    (a.document_type ?? "") === (b.document_type ?? ""));
const months = (i: ChecklistItem) => i.validity_months ?? null;
const count = (i: ChecklistItem) => (hasCount(i.kind) ? (i.min_count ?? 1) : 1);

function notes(was: ChecklistItem, now: ChecklistItem): string[] {
  const n: string[] = [];
  if (was.label.trim() !== now.label.trim())
    n.push(`renamed from "${was.label}"`);
  if (was.is_mandatory !== now.is_mandatory)
    n.push(now.is_mandatory ? "now required" : "now optional");
  if (count(was) !== count(now))
    n.push(`needs ${count(now)} (was ${count(was)})`);
  if (months(was) !== months(now))
    n.push(
      months(now) === null
        ? "no longer expires"
        : months(was) === null
          ? `now expires after ${months(now)} months`
          : `expires after ${months(now)} months (was ${months(was)})`,
    );
  return n;
}

/**
 * Lines of `next` compared with `current`: same, added or changed in next's order, then what was
 * removed. Items pair up by exact name first, then by what they are (type and document).
 */
export function diff(
  current: ChecklistItem[],
  next: ChecklistItem[],
): DiffLine[] {
  const pair = new Map<number, number>(); // next index -> current index
  const used = new Set<number>();
  const norm = (s: string) => s.trim().toLowerCase();
  next.forEach((n, i) => {
    const j = current.findIndex(
      (c, k) =>
        !used.has(k) && sameThing(c, n) && norm(c.label) === norm(n.label),
    );
    if (j >= 0) {
      pair.set(i, j);
      used.add(j);
    }
  });
  next.forEach((n, i) => {
    if (pair.has(i)) return;
    const j = current.findIndex((c, k) => !used.has(k) && sameThing(c, n));
    if (j >= 0) {
      pair.set(i, j);
      used.add(j);
    }
  });
  const lines: DiffLine[] = next.map((item, i) => {
    const j = pair.get(i);
    if (j === undefined) return { change: "added", item };
    const ns = notes(current[j], item);
    return ns.length
      ? { change: "changed", item, was: current[j], notes: ns }
      : { change: "same", item };
  });
  current.forEach((item, k) => {
    if (!used.has(k)) lines.push({ change: "removed", item });
  });
  return lines;
}

/** True when two lists ask for exactly the same things in the same order (what the API refuses to save again). */
export function sameItems(a: ChecklistItem[], b: ChecklistItem[]): boolean {
  const key = (i: ChecklistItem) =>
    [
      i.kind,
      i.document_type ?? null,
      i.label.trim(),
      i.is_mandatory,
      i.min_count ?? 1,
      i.validity_months ?? null,
    ].join("|");
  return a.length === b.length && a.every((x, n) => key(x) === key(b[n]));
}

/** One line describing an item, e.g. "Document upload: National ID · expires after 12 months · optional". */
export function describe(i: ChecklistItem): string {
  const parts = [
    i.kind === "document" && i.document_type
      ? `Upload: ${documentLabel(i.document_type)}`
      : kindLabel(i.kind),
  ];
  if (hasCount(i.kind)) parts.push(`${i.min_count ?? 1} needed`);
  if (i.validity_months)
    parts.push(`expires after ${i.validity_months} months`);
  parts.push(i.is_mandatory ? "required" : "optional");
  return parts.join(" · ");
}

// ---- the Advanced text format: kind | name | required | document type | expiry months | count

export const textHelp =
  "One requirement per line: type | name | required (yes/no) | document type | expires after (months) | how many.";

export function toText(rows: Row[]): string {
  return rows
    .map((r) => {
      const cols = [
        r.kind,
        r.label,
        r.required ? "yes" : "no",
        needsDocument(r.kind) ? r.documentType : "",
        canExpire(r.kind) ? r.validityMonths : "",
        hasCount(r.kind) ? r.minCount : "",
      ];
      while (cols.length > 3 && !cols[cols.length - 1]) cols.pop();
      return cols.join(" | ");
    })
    .join("\n");
}

/** Parses the text format; returns the rows, or the problems by line number. */
export function fromText(text: string): { rows: Row[]; errors: string[] } {
  const rows: Row[] = [];
  const errors: string[] = [];
  text
    .split("\n")
    .map((l) => l.trim())
    .forEach((line, n) => {
      if (!line) return;
      const [
        kind = "",
        label = "",
        req = "",
        doc = "",
        validity = "",
        howMany = "",
      ] = line.split("|").map((s) => s.trim());
      const k = kinds.find(
        (x) => x.value === kind || x.label.toLowerCase() === kind.toLowerCase(),
      )?.value;
      if (!k)
        return errors.push(
          `Line ${n + 1}: unknown type "${kind}". Use one of ${kinds.map((x) => x.value).join(", ")}.`,
        );
      const d = doc
        ? DocumentType.find(
            (x) =>
              x === doc ||
              documentLabels[x].toLowerCase() === doc.toLowerCase(),
          )
        : "";
      if (d === undefined)
        return errors.push(`Line ${n + 1}: unknown document type "${doc}".`);
      rows.push({
        key: newKey(),
        kind: k,
        label,
        required: !/^(no|false|optional)$/i.test(req),
        documentType: d,
        minCount: howMany || (k === "referee" ? "2" : "1"),
        validityMonths: validity,
      });
    });
  return { rows, errors };
}
