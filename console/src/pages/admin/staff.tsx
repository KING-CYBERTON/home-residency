import { useState } from "react";
import { Link } from "react-router";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { $api, type Schemas } from "@/api/client";
import { AccountStatus, StaffRole } from "@/api/enums.gen";
import { DataTable } from "@/components/data-table";
import { EnumFilter, FilterBar } from "@/components/filters";
import { Allowed, FormDialog } from "@/components/form-dialog";
import { PageHeader } from "@/components/page";
import { StatusBadge } from "@/components/status-badge";
import { roleLabels, useCan, useMe } from "@/lib/access";
import { ago } from "@/lib/format";
import { useParamState } from "@/lib/params";
import { SessionsTable } from "@/components/sessions";

const roleOptions = StaffRole.map((r) => ({ value: r, label: roleLabels[r] }));

type Secret = { title: string; password?: string; totp_secret?: string; totp_uri?: string };

export function StaffPage() {
  const me = useMe();
  const [f, set] = useParamState(["role", "status"]);
  const [secret, setSecret] = useState<Secret | null>(null);
  const { data, isLoading, error } = $api.useQuery("get", "/console/staff", {
    params: { query: { role: f.role as Schemas["StaffRole"] | undefined, status: f.status as Schemas["AccountStatus"] | undefined } },
  });
  const create = $api.useMutation("post", "/console/staff");
  const update = $api.useMutation("patch", "/console/staff/{id}");
  const roles = $api.useMutation("put", "/console/staff/{id}/roles");
  const resetMfa = $api.useMutation("post", "/console/staff/{id}/reset-mfa");
  const resetPassword = $api.useMutation("post", "/console/staff/{id}/reset-password");
  const [sessionsOf, setSessionsOf] = useState<Schemas["StaffUser"] | null>(null);
  const can = {
    create: useCan("POST /console/staff"),
    update: useCan("PATCH /console/staff/{id}"),
    roles: useCan("PUT /console/staff/{id}/roles"),
    reset: useCan("POST /console/staff/{id}/reset-mfa"),
    resetPassword: useCan("POST /console/staff/{id}/reset-password"),
    sessions: useCan("GET /console/staff/{id}/sessions"),
  };
  return (
    <>
      <PageHeader
        title="Staff"
        description={
          <>
            Console users and their roles. Admin holds every role. Two-person rules still apply: whoever prepares or investigates cannot also
            approve or decide. <Link to="/roles" className="underline">See what each role can do</Link>.
          </>
        }
        actions={
          <Allowed when={can.create}>
            <FormDialog
              trigger="Invite staff"
              triggerVariant="default"
              title="Invite a staff user"
              fields={[
                { name: "full_name", label: "Full name", required: true },
                { name: "email", label: "Email", required: true },
                { name: "phone", label: "Phone", placeholder: "+2547…" },
                { name: "roles", label: "Roles", type: "multiselect", options: roleOptions, required: true },
              ]}
              onSubmit={(body) => create.mutateAsync({ body })}
              onDone={(u) => {
                const s = u as Schemas["StaffUser"];
                if (s.initial_credentials) setSecret({ title: `Sign-in details for ${s.full_name}`, ...s.initial_credentials });
              }}
            />
          </Allowed>
        }
      />
      <FilterBar>
        <EnumFilter label="Role" value={f.role} options={roleOptions} onChange={(v) => set("role", v)} />
        <EnumFilter label="Status" value={f.status} options={AccountStatus} onChange={(v) => set("status", v)} />
      </FilterBar>
      <DataTable<Schemas["StaffUser"]>
        rows={data}
        loading={isLoading}
        error={error}
        rowKey={(s) => s.id}
        empty="No staff users."
        columns={[
          { header: "Name", cell: (s) => <span>{s.full_name}{s.id === me?.id && <span className="text-muted-foreground text-xs"> (you)</span>}</span> },
          { header: "Email", cell: (s) => s.email },
          { header: "Roles", cell: (s) => <span className="text-xs">{s.roles.map((r) => roleLabels[r]).join(", ")}</span> },
          { header: "Status", cell: (s) => <StatusBadge value={s.status} /> },
          { header: "Last sign-in", cell: (s) => ago(s.last_login_at) },
          {
            header: "",
            cell: (s) => (
              <div className="flex justify-end gap-1">
                <Allowed when={can.roles}>
                  <FormDialog
                    trigger="Roles"
                    title={`Roles for ${s.full_name}`}
                    fields={[{ name: "roles", label: "Roles", type: "multiselect", options: roleOptions, required: true }]}
                    initial={{ roles: s.roles }}
                    submitLabel="Save"
                    onSubmit={(body) => roles.mutateAsync({ params: { path: { id: s.id } }, body })}
                  />
                </Allowed>
                <Allowed when={can.update}>
                  <FormDialog
                    trigger="Edit"
                    title={`Edit ${s.full_name}`}
                    fields={[
                      { name: "full_name", label: "Full name" },
                      { name: "phone", label: "Phone" },
                      { name: "status", label: "Status", type: "select", options: ["active", "suspended", "closed"] },
                    ]}
                    initial={s}
                    submitLabel="Save"
                    onSubmit={(body) => update.mutateAsync({ params: { path: { id: s.id } }, body })}
                  />
                </Allowed>
                <Allowed when={can.reset}>
                  <FormDialog
                    trigger="Reset MFA"
                    title={`Reset ${s.full_name}'s authenticator`}
                    description="Their current authenticator stops working. Give them the new secret in person."
                    destructive
                    submitLabel="Reset"
                    onSubmit={() => resetMfa.mutateAsync({ params: { path: { id: s.id } } })}
                    onDone={(t) => setSecret({ title: `New authenticator for ${s.full_name}`, ...(t as Schemas["TotpEnrolment"]) })}
                  />
                </Allowed>
                <Allowed when={can.resetPassword && s.id !== me?.id}>
                  <FormDialog
                    trigger="Reset password"
                    title={`Reset ${s.full_name}'s password`}
                    description="They get a new temporary password and are signed out everywhere. Their authenticator stays the same."
                    fields={[{ name: "reason", label: "Reason", required: true, placeholder: "Forgot their password" }]}
                    destructive
                    submitLabel="Reset"
                    onSubmit={(body) => resetPassword.mutateAsync({ params: { path: { id: s.id } }, body: body as Schemas["ReasonInput"] })}
                    onDone={(t) => setSecret({ title: `New password for ${s.full_name}`, ...(t as Schemas["TemporaryPassword"]) })}
                  />
                </Allowed>
                <Allowed when={can.sessions}>
                  <Button variant="outline" size="sm" onClick={() => setSessionsOf(s)}>
                    Sessions
                  </Button>
                </Allowed>
              </div>
            ),
          },
        ]}
      />
      <SecretDialog secret={secret} onClose={() => setSecret(null)} />
      <SessionsDialog user={sessionsOf} onClose={() => setSessionsOf(null)} />
    </>
  );
}

/** Credentials are returned once and cannot be fetched again, so we say so plainly. */
function SecretDialog({ secret, onClose }: { secret: Secret | null; onClose: () => void }) {
  return (
    <Dialog open={!!secret} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{secret?.title}</DialogTitle>
          <DialogDescription>Shown only this once. Hand them over in person, not by email or chat.</DialogDescription>
        </DialogHeader>
        <dl className="grid gap-3 text-sm">
          {secret?.password && (
            <div>
              <dt className="text-muted-foreground text-xs">Temporary password</dt>
              <dd className="bg-muted rounded p-2 font-mono select-all">{secret.password}</dd>
            </div>
          )}
          {secret?.totp_secret && (
            <div>
              <dt className="text-muted-foreground text-xs">Authenticator secret</dt>
              <dd className="bg-muted rounded p-2 font-mono break-all select-all">{secret.totp_secret}</dd>
            </div>
          )}
          {secret?.totp_uri && (
            <div>
              <dt className="text-muted-foreground text-xs">Authenticator link (paste into the app or a QR generator)</dt>
              <dd className="bg-muted rounded p-2 font-mono text-xs break-all select-all">{secret.totp_uri}</dd>
            </div>
          )}
        </dl>
        <DialogFooter>
          <Button onClick={onClose}>I have saved these</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Where a colleague is signed in, with a way to end all of it after a lost phone or a shared computer. */
function SessionsDialog({ user, onClose }: { user: Schemas["StaffUser"] | null; onClose: () => void }) {
  const me = useMe();
  const id = user?.id ?? "";
  const { data, isLoading, error } = $api.useQuery("get", "/console/staff/{id}/sessions", { params: { path: { id } } }, { enabled: !!user });
  const signOut = $api.useMutation("post", "/console/staff/{id}/sign-out");
  const canSignOut = useCan("POST /console/staff/{id}/sign-out");
  return (
    <Dialog open={!!user} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Where {user?.full_name} is signed in</DialogTitle>
          <DialogDescription>Signing them out ends every session. They sign in again with their password and authenticator.</DialogDescription>
        </DialogHeader>
        <SessionsTable rows={data} loading={isLoading} error={error} />
        <DialogFooter>
          <Allowed when={canSignOut && !!data?.length && user?.id !== me?.id}>
            <FormDialog
              trigger="Sign out everywhere"
              title={`Sign ${user?.full_name} out everywhere?`}
              fields={[{ name: "reason", label: "Reason", required: true, placeholder: "Lost phone" }]}
              destructive
              submitLabel="Sign out"
              onSubmit={(body) => signOut.mutateAsync({ params: { path: { id } }, body: body as Schemas["ReasonInput"] })}
            />
          </Allowed>
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
