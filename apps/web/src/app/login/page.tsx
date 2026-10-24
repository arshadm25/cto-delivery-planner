"use client";

import { Suspense, useState } from "react";
import { useSearchParams } from "next/navigation";
import { api } from "@/lib/api";

function LoginForm() {
  const params = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api("/auth/login", { method: "POST", body: { email, password } });
      const next = params.get("next");
      // Only same-site relative paths, to avoid an open redirect.
      window.location.href = next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="w-full max-w-sm space-y-5 rounded-2xl border border-slate-200 bg-white p-8 shadow-lg shadow-slate-900/5">
      <div className="space-y-3">
        <div className="grid h-10 w-10 place-items-center rounded-xl bg-blue-600 font-bold text-white" aria-hidden>DP</div>
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900">CTO Delivery Planner</h1>
          <p className="text-sm text-slate-500">Sign in to see delivery forecasts and risks.</p>
        </div>
      </div>
      <label className="block text-sm font-medium text-slate-700">
        Email
        <input type="email" required autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)}
          className="field mt-1 w-full py-2" />
      </label>
      <label className="block text-sm font-medium text-slate-700">
        Password
        <input type="password" required autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)}
          className="field mt-1 w-full py-2" />
      </label>
      {error && <p role="alert" className="text-sm text-red-700">{error}</p>}
      <button disabled={busy} className="btn btn-primary w-full py-2">
        {busy ? "Signing in…" : "Sign in"}
      </button>
    </form>
  );
}

export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <Suspense>
        <LoginForm />
      </Suspense>
    </main>
  );
}
