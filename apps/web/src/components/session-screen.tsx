"use client";
import { useState, type FormEvent } from "react";
import { Activity, LockKeyhole, ShieldAlert } from "lucide-react";
import { APIError } from "@/lib/api";
import { useSession } from "./providers";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

export function SessionScreen() {
  const { session, login, retry } = useSession();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      await login(username.trim(), password);
      setPassword("");
    } catch (cause) {
      setError(
        cause instanceof APIError && cause.status === 401
          ? "Invalid username or password. Check your credentials and try again."
          : "Sign-in is unavailable. Try again shortly.",
      );
    } finally {
      setSubmitting(false);
    }
  };
  return (
    <main className="session-page">
      <section className="session-card" aria-labelledby="session-title">
        <div className="session-brand">
          <Activity aria-hidden="true" /> TelcoPulse
        </div>
        {session.kind === "loading" ? (
          <>
            <h1 id="session-title">Checking operator access</h1>
            <p role="status">Validating your workspace session…</p>
          </>
        ) : session.kind === "unavailable" ? (
          <>
            <ShieldAlert className="session-icon" aria-hidden="true" />
            <h1 id="session-title">Access check unavailable</h1>
            <p>
              TelcoPulse cannot verify operator access right now. Retry when the
              gateway is available.
            </p>
            <Button onClick={() => void retry()}>Retry access check</Button>
          </>
        ) : (
          <>
            <LockKeyhole className="session-icon" aria-hidden="true" />
            <h1 id="session-title">Operator sign in</h1>
            <p>
              Use your TelcoPulse operator account to open the ITOC workspace.
            </p>
            <form
              onSubmit={(event) => void submit(event)}
              className="session-form"
            >
              <div>
                <Label htmlFor="operator-username">Username</Label>
                <Input
                  id="operator-username"
                  name="username"
                  autoComplete="username"
                  required
                  maxLength={60}
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="operator-password">Password</Label>
                <Input
                  id="operator-password"
                  name="password"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                />
              </div>
              {error && (
                <p className="session-error" role="alert">
                  {error}
                </p>
              )}
              <Button type="submit" disabled={submitting}>
                {submitting ? "Signing in…" : "Sign in"}
              </Button>
            </form>
          </>
        )}
        <span className="session-footnote">
          Internal operations · Fictional synthetic environment
        </span>
      </section>
    </main>
  );
}
