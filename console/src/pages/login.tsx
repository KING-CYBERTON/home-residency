import { useLogin } from "@refinedev/core";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { errorMessage, fetchClient, sessionToken, type Schemas } from "@/api/client";

/**
 * Staff sign in with email and password, then a 6-digit code from their
 * authenticator app. The first step returns a session that can only call
 * /auth/staff/mfa; the second upgrades it.
 */
export function LoginPage() {
  const { mutate: login } = useLogin<{ session: Schemas["Session"] }>();
  const [step, setStep] = useState<"password" | "totp">("password");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submitPassword(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const { data } = await fetchClient.POST("/auth/staff/login", { body: { email, password } });
      if (!data) return;
      sessionToken.set(data.token);
      if (data.mfa_required === false) login({ session: data });
      else setStep("totp");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function submitCode(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const { data } = await fetchClient.POST("/auth/staff/mfa", { body: { code } });
      if (data) login({ session: { ...data, mfa_required: false } });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="bg-muted/40 grid min-h-svh place-items-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="flex-col">
          <div className="bg-primary text-primary-foreground mb-2 grid size-9 place-items-center rounded-md text-sm font-bold">
            HR
          </div>
          <CardTitle className="text-xl">Homend Residence console</CardTitle>
          <CardDescription>
            {step === "password" ? "Sign in with your staff account." : "Enter the 6-digit code from your authenticator app."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {step === "password" ? (
            <form onSubmit={submitPassword} className="grid gap-4">
              <div className="grid gap-1.5">
                <Label htmlFor="email">Email</Label>
                <Input id="email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {error && <p role="alert" className="text-destructive text-sm">{error}</p>}
              <Button type="submit" disabled={busy}>
                {busy ? "Signing in…" : "Continue"}
              </Button>
            </form>
          ) : (
            <form onSubmit={submitCode} className="grid gap-4">
              <div className="grid gap-1.5">
                <Label htmlFor="code">Authenticator code</Label>
                <Input
                  id="code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  required
                  autoFocus
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                  className="text-center font-mono text-lg tracking-[0.5em]"
                />
              </div>
              {error && <p role="alert" className="text-destructive text-sm">{error}</p>}
              <Button type="submit" disabled={busy || code.length !== 6}>
                {busy ? "Checking…" : "Sign in"}
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  sessionToken.clear();
                  setStep("password");
                  setCode("");
                  setError(null);
                }}
              >
                Use a different account
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
