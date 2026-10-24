import { ArrowDown, ArrowUp, GripVertical, Plus, Trash2 } from "lucide-react";
import { useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";

import { $api, errorMessage } from "@/api/client";
import { useCategories } from "@/components/lookups";
import { ErrorState, Loading, PageHeader, Section } from "@/components/page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import {
  blankRow,
  canExpire,
  diff,
  documentOptions,
  fromText,
  hasCount,
  itemFromRow,
  kinds,
  needsDocument,
  rowFromItem,
  rowProblems,
  sameItems,
  standard,
  standardKey,
  textHelp,
  toText,
  type ChecklistTemplate,
  type Kind,
  type Row,
} from "./checklist-model";
import { ChecklistDiff, ChecklistPreview } from "./checklists";

/**
 * Drafts a new version of a category's checklist: start from a copy, edit the requirements as rows,
 * and check the preview and the changes from the current version before saving. An approver then
 * makes the draft current from the checklists page.
 */
export function ChecklistEditorPage() {
  const [params] = useSearchParams();
  const categories = useCategories();
  const all = $api.useQuery("get", "/console/checklists", {});
  if (categories.isLoading || all.isLoading) return <Loading />;
  if (categories.error || all.error)
    return <ErrorState error={categories.error ?? all.error} />;
  return (
    <Editor
      templates={all.data ?? []}
      initialCategory={params.get("category_id") ?? ""}
    />
  );
}

type Source = { value: string; label: string; items: () => Row[] };

function Editor({
  templates,
  initialCategory,
}: {
  templates: ChecklistTemplate[];
  initialCategory: string;
}) {
  const navigate = useNavigate();
  const { data: categories = [] } = useCategories();
  const create = $api.useMutation("post", "/console/checklists");
  const catName = (id: string) =>
    categories.find((c) => c.id === id)?.name ?? "this category";

  const [categoryId, setCategoryId] = useState(initialCategory);
  const current = templates.find(
    (t) => t.category_id === categoryId && t.is_current,
  );
  const drafts = templates.filter(
    (t) => t.category_id === categoryId && t.state === "awaiting_approval",
  );

  const sources: Source[] = useMemo(() => {
    const out: Source[] = [];
    if (current)
      out.push({
        value: `t:${current.id}`,
        label: `${catName(categoryId)}: current version (v${current.version})`,
        items: () => current.items.map(rowFromItem),
      });
    for (const d of drafts)
      out.push({
        value: `t:${d.id}`,
        label: `${catName(categoryId)}: draft v${d.version} waiting for approval`,
        items: () => d.items.map(rowFromItem),
      });
    out.push({
      value: "standard",
      label: "Standard (what every provider needs)",
      items: () => standard.map(rowFromItem),
    });
    for (const t of templates)
      if (t.is_current && t.category_id !== categoryId)
        out.push({
          value: `t:${t.id}`,
          label: `Copy from ${catName(t.category_id)} (v${t.version})`,
          items: () => t.items.map(rowFromItem),
        });
    out.push({
      value: "blank",
      label: "Start empty",
      items: () => [blankRow()],
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templates, categoryId, categories]);

  const [from, setFrom] = useState(() => sources[0]?.value ?? "standard");
  const [rows, setRows] = useState<Row[]>(() => sources[0]?.items() ?? []);
  const [touched, setTouched] = useState<Set<string>>(new Set());
  const [submitted, setSubmitted] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);

  function startFrom(value: string) {
    setFrom(value);
    setRows(sources.find((s) => s.value === value)?.items() ?? []);
    setTouched(new Set());
    setSubmitted(false);
  }
  function chooseCategory(id: string) {
    setCategoryId(id);
    const cur = templates.find((t) => t.category_id === id && t.is_current);
    setFrom(cur ? `t:${cur.id}` : "standard");
    setRows((cur ? cur.items : standard).map(rowFromItem));
    setTouched(new Set());
  }

  const problems = rowProblems(rows);
  const items = rows.map(itemFromRow);
  const changes = diff(current?.items ?? [], items);
  const unchanged = Boolean(current) && sameItems(current!.items, items);

  const update = (key: string, patch: Partial<Row>) => {
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
    setTouched((t) => new Set(t).add(key));
  };
  const move = (from: number, to: number) =>
    setRows((rs) => {
      if (to < 0 || to >= rs.length) return rs;
      const next = [...rs];
      const [r] = next.splice(from, 1);
      next.splice(to, 0, r);
      return next;
    });

  async function save() {
    setSubmitted(true);
    setServerError(null);
    if (
      !categoryId ||
      Object.keys(problems).length ||
      !rows.length ||
      unchanged
    )
      return;
    try {
      const saved = await create.mutateAsync({
        body: { category_id: categoryId, items },
      });
      toast.success(`Draft v${saved.version} saved`);
      navigate(`/checklists?category_id=${categoryId}&saved=${saved.id}`);
    } catch (err) {
      setServerError(errorMessage(err));
    }
  }

  const blocking = !categoryId
    ? "Choose the category this checklist is for."
    : !rows.length
      ? "Add at least one requirement."
      : Object.keys(problems).length
        ? "Fix the highlighted requirements before saving."
        : unchanged
          ? `Nothing has changed from the current version (v${current!.version}). Change at least one requirement.`
          : null;

  return (
    <>
      <PageHeader
        title="Draft a checklist version"
        description="What applicants for this category must provide. Saving creates a draft; an approver makes it current. Open applications keep the version they started with."
        back={{
          to: categoryId
            ? `/checklists?category_id=${categoryId}`
            : "/checklists",
          label: "Vetting checklists",
        }}
      />
      <div className="mb-4 grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="cl-category">Category</Label>
          <NativeSelect
            id="cl-category"
            value={categoryId}
            onChange={(e) => chooseCategory(e.target.value)}
          >
            <option value="">Choose…</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
                {c.is_active ? "" : " (inactive)"}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="cl-from">Start from</Label>
          <NativeSelect
            id="cl-from"
            value={from}
            onChange={(e) => startFrom(e.target.value)}
            disabled={!categoryId}
          >
            {sources.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </NativeSelect>
          <p className="text-muted-foreground text-xs">
            Choosing another starting point replaces the requirements below.
          </p>
        </div>
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Section
          title="Requirements"
          actions={
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRows((rs) => [...rs, blankRow()])}
            >
              <Plus /> Add requirement
            </Button>
          }
        >
          <RowList
            rows={rows}
            setRows={setRows}
            update={update}
            move={move}
            problems={problems}
            show={(k) => submitted || touched.has(k)}
          />
          <Advanced
            rows={rows}
            onApply={(r) => {
              setRows(r);
              setTouched(new Set(r.map((x) => x.key)));
            }}
          />
        </Section>

        <div className="space-y-4">
          <Section title="Preview">
            <ChecklistPreview items={items.filter((i) => i.label)} />
          </Section>
          <Section
            title={
              current
                ? `Changes from the current version (v${current.version})`
                : "Changes"
            }
          >
            {current ? (
              <ChecklistDiff lines={changes} unchanged={unchanged} />
            ) : (
              <p className="text-muted-foreground text-sm">
                {categoryId
                  ? `${catName(categoryId)} has no current checklist, so everything here is new.`
                  : "Choose a category."}
              </p>
            )}
          </Section>
        </div>
      </div>

      <div className="bg-background sticky bottom-0 mt-4 flex flex-wrap items-center justify-end gap-3 border-t py-3">
        {(submitted || unchanged) && blocking && (
          <p role="alert" className="text-destructive mr-auto text-sm">
            {blocking}
          </p>
        )}
        {serverError && (
          <p role="alert" className="text-destructive mr-auto text-sm">
            {serverError}
          </p>
        )}
        <Button onClick={save} disabled={create.isPending}>
          {create.isPending ? "Saving…" : "Save draft"}
        </Button>
      </div>
    </>
  );
}

function RowList({
  rows,
  setRows,
  update,
  move,
  problems,
  show,
}: {
  rows: Row[];
  setRows: Dispatch<SetStateAction<Row[]>>;
  update: (key: string, patch: Partial<Row>) => void;
  move: (from: number, to: number) => void;
  problems: Record<string, string[]>;
  show: (key: string) => boolean;
}) {
  // Only the handle is draggable, so text in a row's inputs can still be selected; the whole row is
  // shown as the drag image and moves as it passes over the others.
  const [dragging, setDragging] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<string | null>(null);

  if (!rows.length)
    return (
      <p className="text-muted-foreground py-6 text-center text-sm">
        No requirements yet. Add one, or choose where to start from.
      </p>
    );
  return (
    <ol className="space-y-3">
      {rows.map((r, n) => {
        const std = standardKey(itemFromRow(r, n));
        const shown = show(r.key) ? problems[r.key] : undefined;
        return (
          <li
            key={r.key}
            aria-label={`Requirement ${n + 1}`}
            onDragOver={(e) => {
              if (!dragging) return;
              e.preventDefault();
              if (dragging === r.key) return;
              // Drop before or after this row depending on which half the pointer is in, so rows of
              // different heights don't swap back and forth. Positions come from the latest rows:
              // dragover fires faster than React re-renders.
              const box = e.currentTarget.getBoundingClientRect();
              const after = e.clientY > box.top + box.height / 2;
              setRows((rs) => {
                const moving = rs.find((x) => x.key === dragging);
                if (!moving) return rs;
                const next = rs.filter((x) => x.key !== dragging);
                next.splice(
                  next.findIndex((x) => x.key === r.key) + (after ? 1 : 0),
                  0,
                  moving,
                );
                return next.every((x, i) => x === rs[i]) ? rs : next;
              });
            }}
            onDrop={(e) => e.preventDefault()}
            onDragEnd={() => setDragging(null)}
            className={cn(
              "rounded-md border p-3",
              dragging === r.key && "opacity-50",
              shown && "border-destructive",
            )}
          >
            <div className="flex flex-wrap items-start gap-2">
              <button
                type="button"
                aria-label="Drag to reorder"
                className="text-muted-foreground mt-2 cursor-grab"
                draggable
                onDragStart={(e) => {
                  setDragging(r.key);
                  e.dataTransfer.effectAllowed = "move";
                  e.dataTransfer.setData("text/plain", r.label);
                  const row = e.currentTarget.closest("li");
                  if (row) e.dataTransfer.setDragImage(row, 16, 16);
                }}
              >
                <GripVertical className="size-4" />
              </button>
              <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-[minmax(0,11rem)_minmax(0,1fr)]">
                <NativeSelect
                  aria-label="Type"
                  value={r.kind}
                  onChange={(e) => {
                    const kind = e.target.value as Kind;
                    update(r.key, {
                      kind,
                      minCount: hasCount(kind) ? r.minCount || "2" : r.minCount,
                    });
                  }}
                >
                  {kinds.map((k) => (
                    <option key={k.value} value={k.value}>
                      {k.label}
                    </option>
                  ))}
                </NativeSelect>
                <Input
                  aria-label="Name"
                  placeholder="What the applicant sees, e.g. National ID (both sides)"
                  value={r.label}
                  onChange={(e) => update(r.key, { label: e.target.value })}
                />
              </div>
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Move up"
                  disabled={n === 0}
                  onClick={() => move(n, n - 1)}
                >
                  <ArrowUp />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Move down"
                  disabled={n === rows.length - 1}
                  onClick={() => move(n, n + 1)}
                >
                  <ArrowDown />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="Remove"
                  onClick={() =>
                    std
                      ? setConfirm(r.key)
                      : setRows((rs) => rs.filter((x) => x.key !== r.key))
                  }
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 pl-6 text-sm">
              <label className="flex items-center gap-2">
                <Checkbox
                  aria-label="Required"
                  checked={r.required}
                  onCheckedChange={(c) =>
                    update(r.key, { required: Boolean(c) })
                  }
                />
                Required
              </label>
              {needsDocument(r.kind) && (
                <div className="w-56">
                  <NativeSelect
                    aria-label="Which document"
                    value={r.documentType}
                    onChange={(e) =>
                      update(r.key, { documentType: e.target.value })
                    }
                  >
                    <option value="">Which document…</option>
                    {documentOptions.map((d) => (
                      <option key={d.value} value={d.value}>
                        {d.label}
                      </option>
                    ))}
                  </NativeSelect>
                </div>
              )}
              {hasCount(r.kind) && (
                <label className="flex items-center gap-2">
                  How many
                  <Input
                    aria-label="How many"
                    type="number"
                    min={1}
                    max={10}
                    className="w-16"
                    value={r.minCount}
                    onChange={(e) =>
                      update(r.key, { minCount: e.target.value })
                    }
                  />
                </label>
              )}
              {canExpire(r.kind) && (
                <label className="flex items-center gap-2">
                  Expires after
                  <Input
                    aria-label="Expires after (months)"
                    type="number"
                    min={1}
                    max={120}
                    placeholder="never"
                    className="w-20"
                    value={r.validityMonths}
                    onChange={(e) =>
                      update(r.key, { validityMonths: e.target.value })
                    }
                  />
                  months
                </label>
              )}
              {std && <Badge variant="secondary">Standard</Badge>}
            </div>
            {confirm === r.key && (
              <div
                role="alert"
                className="mt-3 flex flex-wrap items-center gap-2 rounded-md bg-amber-100 p-2 text-sm text-amber-900 dark:bg-amber-900/40 dark:text-amber-200"
              >
                This is a Standard requirement that every provider is normally
                asked for. Remove it anyway?
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => {
                    setRows((rs) => rs.filter((x) => x.key !== r.key));
                    setConfirm(null);
                  }}
                >
                  Remove anyway
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => setConfirm(null)}
                >
                  Keep it
                </Button>
              </div>
            )}
            {shown && (
              <ul className="text-destructive mt-2 pl-6 text-xs">
                {shown.map((p) => (
                  <li key={p}>{p}</li>
                ))}
              </ul>
            )}
          </li>
        );
      })}
    </ol>
  );
}

/** The old one-line-per-item format, for pasting a list in or copying one out. */
function Advanced({
  rows,
  onApply,
}: {
  rows: Row[];
  onApply: (rows: Row[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [errors, setErrors] = useState<string[]>([]);
  return (
    <details
      className="mt-4 text-sm"
      open={open}
      onToggle={(e) => {
        const next = (e.target as HTMLDetailsElement).open;
        setOpen(next);
        if (next) {
          setText(toText(rows));
          setErrors([]);
        }
      }}
    >
      <summary className="text-muted-foreground cursor-pointer">
        Advanced: edit as text
      </summary>
      <p className="text-muted-foreground mt-2 text-xs">{textHelp}</p>
      <Textarea
        aria-label="Requirements as text"
        rows={8}
        className="mt-2 font-mono text-xs"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      {errors.length > 0 && (
        <ul role="alert" className="text-destructive mt-2 text-xs">
          {errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      )}
      <Button
        className="mt-2"
        variant="outline"
        size="sm"
        onClick={() => {
          const parsed = fromText(text);
          setErrors(parsed.errors);
          if (!parsed.errors.length) {
            onApply(parsed.rows);
            setOpen(false);
          }
        }}
      >
        Use this text
      </Button>
    </details>
  );
}
