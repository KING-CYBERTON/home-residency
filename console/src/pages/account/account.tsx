import { useState, type FormEvent } from "react";
import { toast } from "sonner";

import { $api, errorMessage } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FormDialog } from "@/components/form-dialog";
import { DetailList, PageHeader, Section } from "@/components/page";
import { SessionsTable } from "@/components/sessions";
import { roleLabels, useMe, type StaffRole } from "@/lib/access";

export function AccountPage() {
  const me = useMe();
  const sessions = $api.useQuery("get", "/me/sessions", {});
  const end = $api.useMutation("delete", "/me/sessions/{id}");
  return (
    <>
      <PageHeader title="My account" description="Your sign-in details and where you are signed in." />
      <div className="grid gap-6">
        <Section title="You">
          <DetailList
            items={[
              ["Name", me?.full_name],
              ["Email", me?.email],
              ["Roles", (me?.roles ?? []).map((r) => roleLabels[r as StaffRole] ?? r).join(", ")],
            ]}
          />
        </Section>
        <Section title="Change password">
          <PasswordForm />
        </Section>
        <Section title="Where you are signed in">
          <p className="text-muted-foreground mb-3 text-sm">If you see a browser you don't recognise, sign it out and change your password.</p>
          <SessionsTable
            rows={sessions.data}
            loading={sessions.isLoading}
            error={sessions.error}
            end={(s) =>
              !s.current && (
                <FormDialog
                  trigger="Sign out"
                  title="Sign out this browser?"
                  description="It will need your password and authenticator to sign in again."
                  destructive
                  submitLabel="Sign out"
                  onSubmit={() => end.mutateAsync({ params: { path: { id: s.id } } })}
                />
              )
            }
          />
        </Section>
      </div>
    </>
  );
}

function PasswordForm() {
  const change = $api.useMutation("post", "/me/password");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [error, setError] = useState<string | null>(null);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (next.length < 8) return setError("Use at least 8 characters.");
    if (next !== again) return setError("The new passwords do not match.");
    if (next === current) return setError("Choose a password you have not used here.");
    try {
      await change.mutateAsync({ body: { current_password: current, new_password: next } });
      setCurrent("");
      setNext("");
      setAgain("");
      toast.success("Password changed. Your other browsers have been signed out.");
    } catch (err) {
      setError(errorMessage(err));
    }
  }
  return (
    <form onSubmit={submit} className="grid max-w-sm gap-3">
      <div className="grid gap-1.5">
        <Label htmlFor="current-password">Current password</Label>
        <Input id="current-password" type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="new-password">New password</Label>
        <Input id="new-password" type="password" autoComplete="new-password" required value={next} onChange={(e) => setNext(e.target.value)} />
        <p className="text-muted-foreground text-xs">At least 8 characters. A short sentence is easier to remember.</p>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="new-password-again">New password again</Label>
        <Input id="new-password-again" type="password" autoComplete="new-password" required value={again} onChange={(e) => setAgain(e.target.value)} />
      </div>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      <Button type="submit" className="w-fit" disabled={change.isPending}>
        Change password
      </Button>
    </form>
  );
}
