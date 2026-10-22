"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { Card, ErrorBox, Loading } from "@/components/ui";

interface Rules {
  stale_after_days: number;
  milestone_warn_days: number;
  high_priorities: string[];
  release_keywords: string[];
}

export default function SettingsPage() {
  const { data, error, loading } = useApi<Rules>("/settings/rules");
  const [form, setForm] = useState<Rules | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<Error | null>(null);
  useEffect(() => { if (data) setForm(data); }, [data]);
  if (loading && !form) return <Loading />;
  if (error || !form) return <ErrorBox error={error} />;

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    try {
      await api("/settings/rules", { method: "PUT", body: form });
      setMsg("Saved. Flags and health use the new thresholds now.");
    } catch (e) {
      setErr(e as Error);
    }
  }
  const list = (v: string) => v.split(",").map((s) => s.trim()).filter(Boolean);

  return (
    <div className="max-w-xl space-y-4">
      <h1 className="text-xl font-semibold">Rule settings</h1>
      <p className="text-sm text-slate-600">These are starting values, not agreed policy. Every change is recorded in the audit log.</p>
      <Card>
        <form onSubmit={save} className="space-y-3 text-sm">
          <label className="block">Stale after (days without an update)
            <input type="number" min={1} max={365} value={form.stale_after_days} onChange={(e) => setForm({ ...form, stale_after_days: +e.target.value })} className="mt-1 w-full rounded border px-2 py-1" />
          </label>
          <label className="block">Check milestones due within (days)
            <input type="number" min={1} max={365} value={form.milestone_warn_days} onChange={(e) => setForm({ ...form, milestone_warn_days: +e.target.value })} className="mt-1 w-full rounded border px-2 py-1" />
          </label>
          <label className="block">Priorities treated as critical when blocked (comma separated, as named in Redmine)
            <input value={form.high_priorities.join(", ")} onChange={(e) => setForm({ ...form, high_priorities: list(e.target.value) })} className="mt-1 w-full rounded border px-2 py-1" />
          </label>
          <label className="block">Release-step keywords in milestone names
            <input value={form.release_keywords.join(", ")} onChange={(e) => setForm({ ...form, release_keywords: list(e.target.value) })} className="mt-1 w-full rounded border px-2 py-1" />
          </label>
          <button className="rounded bg-slate-900 px-3 py-1.5 text-white">Save</button>
          {msg && <p className="text-emerald-700">{msg}</p>}
          <ErrorBox error={err} />
        </form>
      </Card>
    </div>
  );
}
