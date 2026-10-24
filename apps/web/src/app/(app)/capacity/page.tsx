"use client";

import { useState } from "react";
import { api, Person, Portfolio, Team } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date, hours } from "@/lib/format";
import { can, useMe } from "@/components/Shell";
import { Card, ErrorBox, Loading, Table } from "@/components/ui";
import { TeamsCard, teamWeeklyHours } from "@/components/TeamEditor";

interface Allocation {
  id: number;
  person_id: number | null;
  person_name: string | null;
  team_id: number | null;
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
  const people = useApi<{ people: Person[] }>("/people");
  const teams = useApi<{ teams: Team[] }>("/teams");
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
      <h1 className="text-xl font-semibold">Capacity allocations</h1>
      <p className="text-sm text-slate-600">
        Available and allocated hours per person or team and period. Only approved allocations feed forecasts and the workload view.
        Changing an approved allocation sends it back to draft. Showing {data.from} to {data.to}.
      </p>
      <ErrorBox error={err} />
      <TeamsCard teams={teams.data?.teams ?? []} people={people.data?.people ?? []} onChange={teams.reload} />
      <NewAllocation projects={portfolio.data?.projects.map((p) => ({ id: p.id, name: p.name })) ?? []} people={people.data?.people ?? []}
        teams={teams.data?.teams ?? []} onDone={reload} />
      <Card>
        <Table head={["Who", "Period", "Available", "Allocated by project", "Assumptions", "Status", ""]}>
          {data.allocations.map((a) => (
            <tr key={a.id}>
              <td className="px-2 py-2">{a.person_name ?? (a.team_name ? `Team: ${a.team_name}` : `User ${a.person_id}`)}</td>
              <td className="px-2 py-2">{date(a.period_start)} to {date(a.period_end)}</td>
              <td className="px-2 py-2">{a.available_hours === null ? "Not set" : hours(a.available_hours)}</td>
              <td className="px-2 py-2">{Object.entries(a.projects).map(([pid, h]) => <div key={pid}>{names.get(Number(pid)) ?? `Project ${pid}`}: {hours(h)}</div>)}</td>
              <td className="px-2 py-2 text-slate-600">{a.assumptions}</td>
              <td className="px-2 py-2">{a.status}{a.approved_by ? ` by ${a.approved_by}` : ""}</td>
              <td className="px-2 py-2 space-x-1 whitespace-nowrap">
                {a.status === "draft" && can(me, "approve_capacity") && <button className="rounded bg-slate-900 px-2 py-0.5 text-white" onClick={() => act(() => api(`/capacity-allocations/${a.id}/approve`, { method: "POST" }))}>Approve</button>}
                {a.status === "draft" && <button className="rounded border px-2 py-0.5" onClick={() => act(() => api(`/capacity-allocations/${a.id}`, { method: "DELETE" }))}>Delete</button>}
              </td>
            </tr>
          ))}
        </Table>
        {data.allocations.length === 0 && <p className="text-sm text-slate-500">No allocations in this period. Forecasts fall back to recent throughput or TBD.</p>}
      </Card>
    </div>
  );
}

// Working weekdays (Mon-Fri) between two YYYY-MM-DD dates, inclusive. Only
// used to suggest a team's available hours; the calendar in Settings decides
// forecasts.
function weekdays(start: string, end: string): number {
  if (!start || !end) return 0;
  let n = 0;
  for (let d = new Date(start + "T00:00:00Z"); d <= new Date(end + "T00:00:00Z"); d.setUTCDate(d.getUTCDate() + 1)) {
    const day = d.getUTCDay();
    if (day !== 0 && day !== 6) n++;
  }
  return n;
}

function NewAllocation({ projects, people, teams, onDone }: { projects: { id: number; name: string }[]; people: Person[]; teams: Team[]; onDone: () => void }) {
  // "p:<id>" is a person, "t:<id>" a planner team.
  const [who, setWho] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [available, setAvailable] = useState("");
  const [assumptions, setAssumptions] = useState("");
  const [alloc, setAlloc] = useState<Record<number, string>>({});
  const [err, setErr] = useState<Error | null>(null);
  const team = who.startsWith("t:") ? teams.find((t) => t.id === Number(who.slice(2))) : undefined;
  const weekly = team ? teamWeeklyHours(team) : null;
  const suggestion = weekly !== null && start && end ? Math.round((weekly / 5) * weekdays(start, end) * 10) / 10 : null;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    const projectHours: Record<number, number> = {};
    for (const [k, v] of Object.entries(alloc)) if (v !== "") projectHours[Number(k)] = Number(v);
    try {
      await api("/capacity-allocations", { method: "POST", body: {
        person_id: who.startsWith("p:") ? Number(who.slice(2)) : null, team_id: who.startsWith("t:") ? Number(who.slice(2)) : null,
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
          <select required aria-label="Person or team" value={who} onChange={(e) => setWho(e.target.value)} className="rounded border px-2 py-1">
            <option value="">Person or team…</option>
            {teams.length > 0 && <optgroup label="Teams">{teams.map((t) => <option key={t.id} value={`t:${t.id}`}>{t.name}</option>)}</optgroup>}
            <optgroup label="People">{people.map((p) => <option key={p.id} value={`p:${p.id}`}>{p.name}</option>)}</optgroup>
          </select>
          <input required type="date" aria-label="Period start" value={start} onChange={(e) => setStart(e.target.value)} className="rounded border px-2 py-1" />
          <input required type="date" aria-label="Period end" value={end} onChange={(e) => setEnd(e.target.value)} className="rounded border px-2 py-1" />
          <input type="number" min={0} step="0.5" placeholder="Available hours" value={available} onChange={(e) => setAvailable(e.target.value)} className="w-36 rounded border px-2 py-1" />
        </div>
        {suggestion !== null && available === "" && (
          <p className="text-xs text-slate-600">
            Suggested from the team’s usual hours: {suggestion} h ({weekdays(start, end)} weekdays).{" "}
            <button type="button" className="text-sky-700 hover:underline" onClick={() => setAvailable(String(suggestion))}>Use it</button>
          </p>
        )}
        <input placeholder="Assumptions (leave, part-time, other commitments)" value={assumptions} onChange={(e) => setAssumptions(e.target.value)} className="w-full rounded border px-2 py-1" />
        <div className="grid gap-1 md:grid-cols-3">
          {projects.map((p) => (
            <label key={p.id} className="flex items-center justify-between gap-2">
              <span className="truncate">{p.name}</span>
              <input type="number" min={0} step="0.5" placeholder="h" value={alloc[p.id] ?? ""} onChange={(e) => setAlloc({ ...alloc, [p.id]: e.target.value })} className="w-20 rounded border px-2 py-0.5" />
            </label>
          ))}
        </div>
        <button className="rounded bg-slate-900 px-3 py-1 text-white">Save draft</button>
        <ErrorBox error={err} />
      </form>
    </Card>
  );
}
