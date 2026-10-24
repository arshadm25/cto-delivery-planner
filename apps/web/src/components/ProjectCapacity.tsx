"use client";
import { useState } from "react";
import Link from "next/link";
import { api, Person, Team } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date, hours } from "@/lib/format";
import { can, useMe } from "./Shell";
import { Badge, Card, Empty, ErrorBox, Table } from "./ui";

// Capacity for one project: the allocations that give it hours, and a quick
// way to add one without leaving the project. A person's or team's total
// availability across projects is set on the Capacity page.

interface Allocation {
  id: number;
  person_name: string | null;
  person_id: number | null;
  team_name: string | null;
  period_start: string;
  period_end: string;
  available_hours: number | null;
  status: "draft" | "approved";
  projects: Record<string, number>;
}

export function ProjectCapacity({ projectId, projectIds, onChange }: { projectId: number; projectIds: number[]; onChange: () => void }) {
  const me = useMe();
  const { data, error, reload } = useApi<{ allocations: Allocation[]; from: string; to: string }>("/capacity-allocations");
  const people = useApi<{ people: Person[] }>("/people");
  const teams = useApi<{ teams: Team[] }>("/teams");
  const [err, setErr] = useState<Error | null>(null);

  async function act(fn: () => Promise<unknown>) {
    setErr(null);
    try { await fn(); reload(); onChange(); } catch (e) { setErr(e as Error); }
  }

  // A parent is planned with its subprojects, so their hours count too.
  const ids = new Set(projectIds.map(String));
  const rows = (data?.allocations ?? []).filter((a) => Object.keys(a.projects).some((p) => ids.has(p)));
  const here = (a: Allocation) => Object.entries(a.projects).filter(([p]) => ids.has(p)).reduce((s, [, h]) => s + h, 0);

  return (
    <Card title="Capacity for this project" action={<Link href="/capacity" className="link text-sm">All capacity and teams ›</Link>}>
      <p className="-mt-1 mb-3 text-sm text-slate-500">
        Only approved allocations feed the forecast.{projectIds.length > 1 && " Hours given to subprojects count here too."}
        {data && <> Showing {date(data.from)} to {date(data.to)}.</>}
      </p>
      <ErrorBox error={err ?? error} />
      <Table head={["Who", "Period", "Hours here", "Available in total", "Status", ""]}>
        {rows.map((a) => (
          <tr key={a.id}>
            <td className="px-2 py-2">{a.person_name ?? (a.team_name ? `Team: ${a.team_name}` : `User ${a.person_id}`)}</td>
            <td className="whitespace-nowrap px-2 py-2">{date(a.period_start)} to {date(a.period_end)}</td>
            <td className="px-2 py-2 tabular-nums">{hours(here(a))}</td>
            <td className="px-2 py-2 tabular-nums">{a.available_hours === null ? "Not set" : hours(a.available_hours)}</td>
            <td className="px-2 py-2"><Badge tone={a.status === "approved" ? "ok" : "warn"}>{a.status}</Badge></td>
            <td className="px-2 py-2">
              {a.status === "draft" && can(me, "approve_capacity") && (
                <button className="btn btn-primary btn-sm" onClick={() => act(() => api(`/capacity-allocations/${a.id}/approve`, { method: "POST" }))}>Approve</button>
              )}
            </td>
          </tr>
        ))}
      </Table>
      {data && rows.length === 0 && <Empty>No capacity recorded for this project in this period.</Empty>}
      {can(me, "edit_capacity") && (
        <QuickAllocation projectId={projectId} people={people.data?.people ?? []} teams={teams.data?.teams ?? []}
          onSave={(body) => act(() => api("/capacity-allocations", { method: "POST", body }))} />
      )}
    </Card>
  );
}

function QuickAllocation({ projectId, people, teams, onSave }: {
  projectId: number; people: Person[]; teams: Team[]; onSave: (body: unknown) => Promise<void>;
}) {
  const [f, setF] = useState({ who: "", start: "", end: "", hours: "", assumptions: "" });
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    await onSave({
      person_id: f.who.startsWith("p:") ? Number(f.who.slice(2)) : null,
      team_id: f.who.startsWith("t:") ? Number(f.who.slice(2)) : null,
      period_start: f.start, period_end: f.end, available_hours: null,
      assumptions: f.assumptions, projects: { [projectId]: Number(f.hours) },
    });
    setF({ who: "", start: "", end: "", hours: "", assumptions: "" });
  }
  return (
    <form onSubmit={submit} className="mt-4 rounded-lg border border-dashed border-slate-300 bg-slate-50 p-3 text-sm">
      <div className="mb-2 font-medium text-slate-700">Give this project hours (saved as a draft for the CTO to approve)</div>
      <div className="flex flex-wrap items-end gap-2">
        <select required aria-label="Who" value={f.who} onChange={(e) => setF({ ...f, who: e.target.value })} className="field">
          <option value="">Person or team…</option>
          {teams.length > 0 && <optgroup label="Teams">{teams.map((t) => <option key={t.id} value={`t:${t.id}`}>{t.name}</option>)}</optgroup>}
          <optgroup label="People">{people.map((p) => <option key={p.id} value={`p:${p.id}`}>{p.name}</option>)}</optgroup>
        </select>
        <input required type="date" aria-label="From" value={f.start} onChange={(e) => setF({ ...f, start: e.target.value })} className="field" />
        <input required type="date" aria-label="To" value={f.end} onChange={(e) => setF({ ...f, end: e.target.value })} className="field" />
        <input required type="number" min={0} step="0.5" aria-label="Hours on this project" placeholder="hours here" value={f.hours} onChange={(e) => setF({ ...f, hours: e.target.value })} className="field w-28" />
        <input aria-label="Assumptions" placeholder="Assumptions (optional)" value={f.assumptions} onChange={(e) => setF({ ...f, assumptions: e.target.value })} className="field min-w-48 flex-1" />
        <button className="btn btn-primary">Save draft</button>
      </div>
    </form>
  );
}
