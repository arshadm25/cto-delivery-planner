"use client";

import { useState } from "react";
import { api, Portfolio } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date, hours } from "@/lib/format";
import { can, useMe } from "@/components/Shell";
import { Card, ErrorBox, Loading, Table } from "@/components/ui";

interface Allocation {
  id: number;
  person_id: number | null;
  person_name: string | null;
  team_name: string | null;
  period_start: string;
  period_end: string;
  available_hours: number | null;
  assumptions: string;
  status: "draft" | "approved";
  created_by: string;
  approved_by: string | null;
  projects: Record<string, number>;
}

export default function CapacityPage() {
  const me = useMe();
  const { data, error, loading, reload } = useApi<{ allocations: Allocation[]; from: string; to: string }>("/capacity-allocations");
  const portfolio = useApi<Portfolio>("/portfolio/summary");
  const [err, setErr] = useState<Error | null>(null);
  const names = new Map((portfolio.data?.projects ?? []).map((p) => [p.id, p.name]));
  async function act(fn: () => Promise<unknown>) {
    setErr(null);
    try { await fn(); reload(); } catch (e) { setErr(e as Error); }
  }
  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold tracking-tight text-slate-900">Capacity allocations</h1>
      <p className="text-sm text-slate-600">
        Available and allocated hours per person or team and period. Only approved allocations feed forecasts and the workload view.
        Changing an approved allocation sends it back to draft. Showing {data.from} to {data.to}.
      </p>
      <ErrorBox error={err} />
      <NewAllocation projects={portfolio.data?.projects.map((p) => ({ id: p.id, name: p.name })) ?? []} onDone={reload} />
      <Card>
        <Table head={["Who", "Period", "Available", "Allocated by project", "Assumptions", "Status", ""]}>
          {data.allocations.map((a) => (
            <tr key={a.id}>
              <td className="px-2 py-2">{a.person_name ?? a.team_name ?? `User ${a.person_id}`}</td>
              <td className="px-2 py-2">{date(a.period_start)} to {date(a.period_end)}</td>
              <td className="px-2 py-2">{a.available_hours === null ? "Not set" : hours(a.available_hours)}</td>
              <td className="px-2 py-2">{Object.entries(a.projects).map(([pid, h]) => <div key={pid}>{names.get(Number(pid)) ?? `Project ${pid}`}: {hours(h)}</div>)}</td>
              <td className="px-2 py-2 text-slate-600">{a.assumptions}</td>
              <td className="px-2 py-2">{a.status}{a.approved_by ? ` by ${a.approved_by}` : ""}</td>
              <td className="px-2 py-2 space-x-1 whitespace-nowrap">
                {a.status === "draft" && can(me, "approve_capacity") && <button className="btn btn-primary" onClick={() => act(() => api(`/capacity-allocations/${a.id}/approve`, { method: "POST" }))}>Approve</button>}
                {a.status === "draft" && <button className="btn btn-sm" onClick={() => act(() => api(`/capacity-allocations/${a.id}`, { method: "DELETE" }))}>Delete</button>}
              </td>
            </tr>
          ))}
        </Table>
        {data.allocations.length === 0 && <p className="text-sm text-slate-500">No allocations in this period. Forecasts fall back to recent throughput or TBD.</p>}
      </Card>
    </div>
  );
}

function NewAllocation({ projects, onDone }: { projects: { id: number; name: string }[]; onDone: () => void }) {
  const people = useApi<{ people: { id: number; name: string }[] }>("/people");
  const [who, setWho] = useState("");
  const [team, setTeam] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [available, setAvailable] = useState("");
  const [assumptions, setAssumptions] = useState("");
  const [alloc, setAlloc] = useState<Record<number, string>>({});
  const [err, setErr] = useState<Error | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    const projectHours: Record<number, number> = {};
    for (const [k, v] of Object.entries(alloc)) if (v !== "") projectHours[Number(k)] = Number(v);
    try {
      await api("/capacity-allocations", { method: "POST", body: {
        person_id: who ? Number(who) : null, team_name: who ? null : team || null,
        period_start: start, period_end: end, available_hours: available === "" ? null : Number(available),
        assumptions, projects: projectHours,
      } });
      setAlloc({}); setAvailable(""); setAssumptions("");
      onDone();
    } catch (e) { setErr(e as Error); }
  }

  return (
    <Card title="Add allocation (saved as draft)">
      <form onSubmit={submit} className="space-y-3 text-sm">
        <div className="flex flex-wrap gap-2">
          <select aria-label="Person" value={who} onChange={(e) => setWho(e.target.value)} className="field">
            <option value="">Team (enter name)</option>
            {(people.data?.people ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
          {!who && <input placeholder="Team name" value={team} onChange={(e) => setTeam(e.target.value)} className="field" />}
          <input required type="date" aria-label="Period start" value={start} onChange={(e) => setStart(e.target.value)} className="field" />
          <input required type="date" aria-label="Period end" value={end} onChange={(e) => setEnd(e.target.value)} className="field" />
          <input type="number" min={0} step="0.5" placeholder="Available hours" value={available} onChange={(e) => setAvailable(e.target.value)} className="field w-36" />
        </div>
        <input placeholder="Assumptions (leave, part-time, other commitments)" value={assumptions} onChange={(e) => setAssumptions(e.target.value)} className="field w-full" />
        <div className="grid gap-1 md:grid-cols-3">
          {projects.map((p) => (
            <label key={p.id} className="flex items-center justify-between gap-2">
              <span className="truncate">{p.name}</span>
              <input type="number" min={0} step="0.5" placeholder="h" value={alloc[p.id] ?? ""} onChange={(e) => setAlloc({ ...alloc, [p.id]: e.target.value })} className="field w-20" />
            </label>
          ))}
        </div>
        <button className="btn btn-primary">Save draft</button>
        <ErrorBox error={err} />
      </form>
    </Card>
  );
}
