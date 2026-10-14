import { useCreate, useDelete, useList, useUpdate, type BaseRecord } from "@refinedev/core";
import type { ReactNode } from "react";

import { DataTable, type Column } from "@/components/data-table";
import { Allowed, FormDialog, type Field } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { useCan } from "@/lib/access";

/**
 * Catalogue and content tables share one shape: GET /console/{resource} returns
 * an array, POST creates, PATCH /{id} updates and DELETE /{id} deactivates.
 * These go through Refine's data hooks and the console data provider.
 */
export function CrudPage<T extends BaseRecord & { id?: string | number }>({
  resource,
  title,
  description,
  noun,
  columns,
  fields,
  canDelete = true,
  toForm,
  extraActions,
}: {
  resource: string;
  title: string;
  description?: ReactNode;
  noun: string;
  columns: Column<T>[];
  fields: Field[];
  canDelete?: boolean;
  /** Maps a record to the form's initial values when editing, if they differ. */
  toForm?: (row: T) => Record<string, unknown>;
  extraActions?: (row: T) => ReactNode;
}) {
  const quiet = { successNotification: false as const, errorNotification: false as const };
  const { result, query } = useList<T>({ resource, pagination: { mode: "off" }, ...quiet });
  const { mutateAsync: create } = useCreate();
  const { mutateAsync: update } = useUpdate();
  const { mutateAsync: remove } = useDelete();
  const can = {
    create: useCan(`POST /console/${resource}`),
    update: useCan(`PATCH /console/${resource}/{id}`),
    remove: useCan(`DELETE /console/${resource}/{id}`),
  };

  const actions: Column<T> = {
    header: "",
    cell: (row) => (
      <div className="flex justify-end gap-1">
        {extraActions?.(row)}
        <Allowed when={can.update}>
          <FormDialog
            trigger="Edit"
            title={`Edit ${noun}`}
            fields={fields}
            initial={toForm ? toForm(row) : row}
            submitLabel="Save"
            success={`${capitalise(noun)} saved`}
            onSubmit={(values) => update({ resource, id: row.id!, values, ...quiet })}
          />
        </Allowed>
        <Allowed when={canDelete && can.remove && row.is_active !== false && row.is_published !== false}>
          <FormDialog
            trigger="Deactivate"
            title={`Deactivate ${noun}`}
            description="It stops showing to clients and providers. History is kept."
            destructive
            submitLabel="Deactivate"
            success={`${capitalise(noun)} deactivated`}
            onSubmit={() => remove({ resource, id: row.id!, ...quiet })}
          />
        </Allowed>
      </div>
    ),
  };

  return (
    <>
      <PageHeader
        title={title}
        description={description}
        actions={
          <Allowed when={can.create}>
            <FormDialog
              trigger={`New ${noun}`}
              triggerVariant="default"
              title={`New ${noun}`}
              fields={fields}
              initial={{ is_active: true }}
              submitLabel="Create"
              success={`${capitalise(noun)} created`}
              onSubmit={(values) => create({ resource, values, ...quiet })}
            />
          </Allowed>
        }
      />
      <DataTable<T>
        rows={result.data}
        loading={query.isLoading}
        error={query.error}
        rowKey={(r) => String(r.id)}
        empty={`No ${noun.replace(/([^aeiou])y$/, "$1ie")}s yet.`}
        columns={[...columns, actions]}
      />
    </>
  );
}

function capitalise(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
