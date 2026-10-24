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
  awaiting_qa_statuses: string[];
}

interface Calendar {
  working_days: number[];
  hours_per_day: number;
  holidays: string[];
  confirmed: boolean;
}

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function CalendarCard() {
  const { data, reload } = useApi<Calendar>("/settings/calendar");
  const [form, setForm] = useState<Calendar | null>(null);
  const [holidays, setHolidays] = useState("");
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<Error | null>(null);
  useEffect(() => { if (data) { setForm(data); setHolidays((data.holidays ?? []).join("\n")); } }, [data]);
  if (!form) return null;
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    try {
      await api("/settings/calendar", { method: "PUT", body: { ...form, holidays: holidays.split(/\s+/).filter(Boolean) } });
      setMsg("Calendar saved and marked confirmed.");
      reload();
    } catch (e) { setErr(e as Error); }
  }
  return (
    <Card title={`Working calendar${form.confirmed ? "" : " (not confirmed: defaults in use)"}`}>
      <form onSubmit={save} className="space-y-3 text-sm">
        <div className="flex flex-wrap gap-3">
          {DAYS.map((d, i) => (
            <label key={d} className="flex items-center gap-1">
              <input type="checkbox" checked={form.working_days.includes(i)}
                onChange={(e) => setForm({ ...form, working_days: e.target.checked ? [...form.working_days, i].sort() : form.working_days.filter((x) => x !== i) })} />
              {d}
            </label>
          ))}
        </div>
        <label className="block">Hours per working day
          <input type="number" min={1} max={24} step="0.5" value={form.hours_per_day} onChange={(e) => setForm({ ...form, hours_per_day: +e.target.value })} className="field mt-1 w-full" />
        </label>
        <label className="block">Holidays (one YYYY-MM-DD per line)
          <textarea rows={4} value={holidays} onChange={(e) => setHolidays(e.target.value)} className="field mt-1 w-full font-mono" />
        </label>
        <button className="btn btn-primary">Save calendar</button>
        {msg && <p className="text-emerald-700">{msg}</p>}
        <ErrorBox error={err} />
      </form>
    </Card>
  );
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
      <h1 className="text-2xl font-bold tracking-tight text-slate-900">Rule settings</h1>
      <p className="text-sm text-slate-600">These are starting values, not agreed policy. Every change is recorded in the audit log.</p>
      <Card>
        <form onSubmit={save} className="space-y-3 text-sm">
          <label className="block">Stale after (days without an update)
            <input type="number" min={1} max={365} value={form.stale_after_days} onChange={(e) => setForm({ ...form, stale_after_days: +e.target.value })} className="field mt-1 w-full" />
          </label>
          <label className="block">Check milestones due within (days)
            <input type="number" min={1} max={365} value={form.milestone_warn_days} onChange={(e) => setForm({ ...form, milestone_warn_days: +e.target.value })} className="field mt-1 w-full" />
          </label>
          <label className="block">Priorities treated as critical when blocked (comma separated, as named in Redmine)
            <input value={form.high_priorities.join(", ")} onChange={(e) => setForm({ ...form, high_priorities: list(e.target.value) })} className="field mt-1 w-full" />
          </label>
          <label className="block">Release-step keywords in milestone names
            <input value={form.release_keywords.join(", ")} onChange={(e) => setForm({ ...form, release_keywords: list(e.target.value) })} className="field mt-1 w-full" />
          </label>
          <label className="block">Statuses meaning &quot;awaiting QA&quot; (comma separated, as named in Redmine). These issues stay open but are not counted as remaining development work or flagged as stale.
            <input value={(form.awaiting_qa_statuses ?? []).join(", ")} onChange={(e) => setForm({ ...form, awaiting_qa_statuses: list(e.target.value) })} className="field mt-1 w-full" />
          </label>
          <button className="btn btn-primary">Save</button>
          {msg && <p className="text-emerald-700">{msg}</p>}
          <ErrorBox error={err} />
        </form>
      </Card>
      <CalendarCard />
    </div>
  );
}
