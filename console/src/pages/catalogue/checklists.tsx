import { AlertTriangle, ChevronDown, ChevronRight, X } from "lucide-react";
import { useState } from "react";
import { Link, useSearchParams } from "react-router";

import { $api } from "@/api/client";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { useCategories } from "@/components/lookups";
import { Empty, ErrorState, Loading, PageHeader } from "@/components/page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useCan, useMe } from "@/lib/access";
import { date } from "@/lib/format";
import { cn } from "@/lib/utils";

import {
  describe,
  diff,
  sameItems,
  standardKey,
  vsStandard,
  type ChecklistItem,
  type ChecklistTemplate,
  type DiffLine,
} from "./checklist-model";

/** What an applicant is asked for, in order. */
export function ChecklistPreview({ items }: { items: ChecklistItem[] }) {
  if (!items.length)
    return <p className="text-muted-foreground text-sm">No requirements.</p>;
  return (
    <ol className="list-decimal space-y-1.5 pl-5 text-sm">
      {items.map((i, n) => (
        <li key={i.id ?? n}>
          <span className="font-medium">{i.label}</span>
          {standardKey(i) ? null : (
            <Badge variant="outline" className="ml-2">
              Extra
            </Badge>
          )}
          <div className="text-muted-foreground text-xs">{describe(i)}</div>
        </li>
      ))}
    </ol>
  );
}

const lineStyle: Record<DiffLine["change"], string> = {
  same: "",
  added: "border-l-emerald-500 bg-emerald-50 dark:bg-emerald-950/40",
  removed:
    "border-l-red-500 bg-red-50 line-through decoration-red-500/60 dark:bg-red-950/40",
  changed: "border-l-amber-500 bg-amber-50 dark:bg-amber-950/40",
};
const lineTag: Record<DiffLine["change"], string> = {
  same: "",
  added: "Added",
  removed: "Removed",
  changed: "Changed",
};

/** Each requirement marked added (green), removed (red) or changed (amber) against the current version. */
export function ChecklistDiff({
  lines,
  unchanged,
}: {
  lines: DiffLine[];
  unchanged?: boolean;
}) {
  const changed = lines.filter((l) => l.change !== "same");
  if (unchanged)
    return <p className="text-muted-foreground text-sm">No changes.</p>;
  return (
    <div className="space-y-2">
      {!changed.length && (
        <p className="text-muted-foreground text-sm">
          Same requirements; only their order has changed.
        </p>
      )}
      <ul className="space-y-1 text-sm" aria-label="Changes">
        {lines.map((l, n) => (
          <li
            key={n}
            data-change={l.change}
            className={cn(
              "rounded-sm border-l-4 border-l-transparent px-2 py-1",
              lineStyle[l.change],
            )}
          >
            <div className="flex flex-wrap items-baseline gap-x-2">
              {l.change !== "same" && (
                <span className="text-xs font-semibold uppercase no-underline">
                  {lineTag[l.change]}
                </span>
              )}
              <span
                className={
                  l.change === "same" ? "text-muted-foreground" : "font-medium"
                }
              >
                {l.item.label}
              </span>
            </div>
            {l.change === "changed" && (
              <div className="text-xs">{l.notes.join("; ")}</div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

const stateBadge = (t: ChecklistTemplate) =>
  t.state === "current" ? (
    <Badge variant="success">Current</Badge>
  ) : t.state === "awaiting_approval" ? (
    <Badge variant="warning">Waiting for approval</Badge>
  ) : t.state === "replaced" ? (
    <Badge variant="secondary">Replaced</Badge>
  ) : (
    <Badge variant="secondary">Not approved</Badge>
  );

export function ChecklistsPage() {
  const me = useMe();
  const [params, setParams] = useSearchParams();
  const categories = useCategories();
  const can = {
    create: useCan("POST /console/checklists"),
    publish: useCan("POST /console/checklists/{id}/publish"),
  };
  const { data, isLoading, error } = $api.useQuery(
    "get",
    "/console/checklists",
    {},
  );
  const [open, setOpen] = useState<Set<string>>(
    () =>
      new Set(params.get("category_id") ? [params.get("category_id")!] : []),
  );
  const toggle = (id: string) =>
    setOpen((o) => {
      const next = new Set(o);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  if (isLoading || categories.isLoading) return <Loading />;
  if (error || categories.error)
    return <ErrorState error={error ?? categories.error} />;
  const templates = data ?? [];
  const saved = templates.find((t) => t.id === params.get("saved"));
  const savedCat = categories.data?.find((c) => c.id === saved?.category_id);
  const rows = (categories.data ?? []).map((c) => ({
    category: c,
    current: templates.find((t) => t.category_id === c.id && t.is_current),
    waiting: templates.filter(
      (t) => t.category_id === c.id && t.state === "awaiting_approval",
    ),
    older: templates.filter(
      (t) =>
        t.category_id === c.id &&
        (t.state === "replaced" || t.state === "not_approved"),
    ),
  }));
  const dismiss = () =>
    setParams(
      (p) => {
        p.delete("saved");
        return p;
      },
      { replace: true },
    );

  return (
    <>
      <PageHeader
        title="Vetting checklists"
        description="What each category's applicants must provide. Admins draft a version; an approver makes it current. Open applications keep the version they started with."
        actions={
          <Allowed when={can.create}>
            <Button asChild>
              <Link to="/checklists/new">Draft a checklist</Link>
            </Button>
          </Allowed>
        }
      />

      {saved && saved.state === "awaiting_approval" && (
        <div
          role="status"
          className="mb-4 flex flex-wrap items-center gap-3 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-950/40"
        >
          <span className="mr-auto">
            Draft v{saved.version} of the {savedCat?.name ?? ""} checklist is
            saved. It is waiting for an approver to make it current.
          </span>
          <Allowed when={can.publish}>
            <ApproveButton
              t={saved}
              name={savedCat?.name ?? ""}
              current={
                rows.find((r) => r.category.id === saved.category_id)?.current
              }
            />
          </Allowed>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Dismiss"
            onClick={dismiss}
          >
            <X />
          </Button>
        </div>
      )}

      {!rows.length ? (
        <Empty>No categories yet.</Empty>
      ) : (
        <Card className="divide-y p-0">
          {rows.map(({ category: c, current, waiting, older }) => {
            const isOpen = open.has(c.id);
            const std = current ? vsStandard(current.items) : null;
            return (
              <section key={c.id} aria-label={c.name}>
                <button
                  type="button"
                  aria-expanded={isOpen}
                  onClick={() => toggle(c.id)}
                  className="hover:bg-muted/50 flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 text-left"
                >
                  {isOpen ? (
                    <ChevronDown className="size-4" />
                  ) : (
                    <ChevronRight className="size-4" />
                  )}
                  <span className="font-medium">{c.name}</span>
                  {!c.is_active && (
                    <Badge variant="secondary">Category inactive</Badge>
                  )}
                  {current ? (
                    <>
                      <Badge variant="success">
                        v{current.version} current
                      </Badge>
                      <span className="text-muted-foreground text-sm">
                        {current.items.length} requirement
                        {current.items.length === 1 ? "" : "s"}
                      </span>
                    </>
                  ) : (
                    <span className="flex items-center gap-1 text-sm font-medium text-amber-700 dark:text-amber-300">
                      <AlertTriangle className="size-4" /> No checklist:
                      applicants can't be vetted
                    </span>
                  )}
                  {waiting.map((w) => (
                    <Badge key={w.id} variant="warning">
                      Draft v{w.version} waiting for approval
                    </Badge>
                  ))}
                  {std && (std.missing.length > 0 || std.extra.length > 0) && (
                    <span className="text-muted-foreground ml-auto text-xs">
                      {[
                        std.extra.length
                          ? `${std.extra.length} more than Standard`
                          : "",
                        std.missing.length
                          ? `${std.missing.length} Standard missing`
                          : "",
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </span>
                  )}
                </button>
                {isOpen && (
                  <div className="grid gap-6 px-4 pb-4 lg:grid-cols-2">
                    <div>
                      <h3 className="mb-2 text-sm font-semibold">
                        {current
                          ? `Current version (v${current.version})`
                          : "No current version"}
                      </h3>
                      {current ? (
                        <>
                          <ChecklistPreview items={current.items} />
                          {std && std.missing.length > 0 && (
                            <p className="mt-2 text-xs text-amber-700 dark:text-amber-300">
                              Not asked for, though Standard:{" "}
                              {std.missing.join(", ")}.
                            </p>
                          )}
                          <p className="text-muted-foreground mt-2 text-xs">
                            {current.created_by_name
                              ? `Drafted by ${current.created_by_name}. `
                              : ""}
                            {current.approved_by_name
                              ? `Made current by ${current.approved_by === me?.id ? "you" : current.approved_by_name}${current.approved_at ? ` on ${date(current.approved_at)}` : ""}.`
                              : ""}
                          </p>
                        </>
                      ) : (
                        <p className="text-muted-foreground text-sm">
                          Applications for {c.name} cannot be vetted until a
                          checklist is made current.
                        </p>
                      )}
                      <Allowed when={can.create}>
                        <Button
                          asChild
                          variant="outline"
                          size="sm"
                          className="mt-3"
                        >
                          <Link to={`/checklists/new?category_id=${c.id}`}>
                            {current ? "Edit checklist" : "Create checklist"}
                          </Link>
                        </Button>
                      </Allowed>
                    </div>
                    <div className="space-y-4">
                      {waiting.map((w) => (
                        <div
                          key={w.id}
                          aria-label={`Draft v${w.version}`}
                          className="rounded-md border p-3"
                        >
                          <div className="mb-2 flex flex-wrap items-center gap-2">
                            <h3 className="text-sm font-semibold">
                              Draft v{w.version}
                            </h3>
                            {stateBadge(w)}
                            <span className="text-muted-foreground text-xs">
                              {w.created_by_name
                                ? `by ${w.created_by === me?.id ? "you" : w.created_by_name}, `
                                : ""}
                              {date(w.created_at)}
                            </span>
                          </div>
                          {current ? (
                            <ChecklistDiff
                              lines={diff(current.items, w.items)}
                              unchanged={sameItems(current.items, w.items)}
                            />
                          ) : (
                            <ChecklistPreview items={w.items} />
                          )}
                          <div className="mt-3">
                            {can.publish ? (
                              <ApproveButton
                                t={w}
                                name={c.name}
                                current={current}
                              />
                            ) : (
                              <p className="text-muted-foreground text-xs">
                                An approver makes it current.
                              </p>
                            )}
                          </div>
                        </div>
                      ))}
                      {older.length > 0 && (
                        <p className="text-muted-foreground text-xs">
                          Earlier versions:{" "}
                          {older
                            .map(
                              (o) =>
                                `v${o.version} (${o.state === "replaced" ? "replaced" : "never approved"})`,
                            )
                            .join(", ")}
                          .
                        </p>
                      )}
                    </div>
                  </div>
                )}
              </section>
            );
          })}
        </Card>
      )}
    </>
  );
}

function ApproveButton({
  t,
  name,
  current,
}: {
  t: ChecklistTemplate;
  name: string;
  current?: ChecklistTemplate;
}) {
  const publish = $api.useMutation("post", "/console/checklists/{id}/publish");
  return (
    <FormDialog
      trigger="Approve and make current"
      triggerVariant="default"
      title={`Make v${t.version} of the ${name} checklist current`}
      description={
        current
          ? `New ${name} applications will use v${t.version} instead of v${current.version}. Applications already open keep their version.`
          : `New ${name} applications will use v${t.version}.`
      }
      submitLabel="Approve and make current"
      success={`v${t.version} is now the current ${name} checklist`}
      onSubmit={() => publish.mutateAsync({ params: { path: { id: t.id } } })}
    />
  );
}
