"use client";
import { useState } from "react";
import { api, Person, Team } from "@/lib/api";
import { hours } from "@/lib/format";
import { can, useMe } from "./Shell";
import { Card, ErrorBox, Table } from "./ui";

// Teams are built in the planner from Redmine users. A member's usual hours
// per week suggest a team's available hours when capacity is allocated.

export function teamWeeklyHours(t: Team): number | null {
  const known = t.members.filter((m) => m.hours_per_week !== null);
  return known.length === 0 ? null : known.reduce((s, m) => s + (m.hours_per_week ?? 0), 0);
}

export function TeamsCard({ teams, people, onChange }: { teams: Team[]; people: Person[]; onChange: () => void }) {
  const me = useMe();
  const editable = can(me, "edit_capacity");
  const [editing, setEditing] = useState<number | "new" | null>(null);
  const [err, setErr] = useState<Error | null>(null);

  async function act(fn: () => Promise<unknown>) {
    setErr(null);
    try {
      await fn();
      setEditing(null);
      onChange();
    } catch (e) {
      setErr(e as Error);
    }
  }

  return (
    <Card title="Teams">
      <p className="mb-3 text-sm text-slate-600">Group Redmine members into teams, then allocate capacity to a team below.</p>
      <ErrorBox error={err} />
      <Table head={editable ? ["Team", "Members", "Usual hours per week", "Edit"] : ["Team", "Members", "Usual hours per week"]}>
        {teams.map((t) => editing === t.id ? (
          <tr key={t.id} className="bg-amber-50">
            <td colSpan={4} className="px-2 py-2">
              <TeamForm initial={t} people={people} onCancel={() => setEditing(null)}
                onSubmit={(b) => act(() => api(`/teams/${t.id}`, { method: "PUT", body: b }))} />
            </td>
          </tr>
        ) : (
          <tr key={t.id}>
            <td className="px-2 py-2 font-medium">{t.name}</td>
            <td className="px-2 py-2">
              {t.members.length === 0 ? <span className="text-slate-500">No members</span> :
                t.members.map((m) => <div key={m.person_id}>{m.person_name ?? `User ${m.person_id}`}{m.hours_per_week !== null && <span className="text-slate-500"> · {hours(m.hours_per_week)}/week</span>}</div>)}
            </td>
            <td className="px-2 py-2">{teamWeeklyHours(t) === null ? "Not set" : hours(teamWeeklyHours(t))}</td>
            {editable && (
              <td className="px-2 py-2 space-x-1 whitespace-nowrap">
                <button className="btn btn-sm" onClick={() => setEditing(t.id)}>Edit</button>
                <button className="btn btn-sm" onClick={() => {
                  if (confirm(`Delete team “${t.name}”?`)) act(() => api(`/teams/${t.id}`, { method: "DELETE" }));
                }}>Delete</button>
              </td>
            )}
          </tr>
        ))}
      </Table>
      {teams.length === 0 && <p className="text-sm text-slate-500">No teams yet.</p>}
      {editable && (editing === "new" ? (
        <div className="mt-3">
          <TeamForm people={people} onCancel={() => setEditing(null)} onSubmit={(b) => act(() => api("/teams", { method: "POST", body: b }))} />
        </div>
      ) : (
        <button className="btn mt-3" onClick={() => setEditing("new")}>New team</button>
      ))}
    </Card>
  );
}

type TeamBody = { name: string; members: { person_id: number; hours_per_week: number | null }[] };

function TeamForm({ initial, people, onSubmit, onCancel }: {
  initial?: Team; people: Person[]; onSubmit: (b: TeamBody) => void; onCancel: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [members, setMembers] = useState<{ person_id: number; hours: string }[]>(
    initial?.members.map((m) => ({ person_id: m.person_id, hours: m.hours_per_week === null ? "" : String(m.hours_per_week) })) ?? [],
  );
  const [pick, setPick] = useState("");
  const [filter, setFilter] = useState("");
  const inTeam = new Set(members.map((m) => m.person_id));
  const nameOf = (id: number) => people.find((p) => p.id === id)?.name ?? `User ${id}`;
  const choices = people.filter((p) => !inTeam.has(p.id) && p.name.toLowerCase().includes(filter.toLowerCase()));

  function submit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({ name, members: members.map((m) => ({ person_id: m.person_id, hours_per_week: m.hours === "" ? null : Number(m.hours) })) });
  }
  return (
    <form onSubmit={submit} className="space-y-2 text-sm">
      <input required aria-label="Team name" placeholder="Team name" value={name} onChange={(e) => setName(e.target.value)} className="field" />
      <div className="flex flex-wrap items-center gap-2">
        <input aria-label="Find member" placeholder="Find a Redmine member" value={filter} onChange={(e) => setFilter(e.target.value)} className="field" />
        <select aria-label="Member to add" value={pick} onChange={(e) => setPick(e.target.value)} className="field">
          <option value="">Choose…</option>
          {choices.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
        <button type="button" disabled={!pick} className="btn" onClick={() => {
          setMembers([...members, { person_id: Number(pick), hours: "" }]);
          setPick("");
        }}>Add member</button>
      </div>
      {members.length > 0 && (
        <ul className="space-y-1">
          {members.map((m, i) => (
            <li key={m.person_id} className="flex items-center gap-2">
              <span className="w-48 truncate">{nameOf(m.person_id)}</span>
              <input type="number" min={0} max={168} step="0.5" aria-label={`Hours per week for ${nameOf(m.person_id)}`} placeholder="h/week"
                value={m.hours} onChange={(e) => setMembers(members.map((x, j) => j === i ? { ...x, hours: e.target.value } : x))} className="field w-24" />
              <button type="button" className="link text-xs" onClick={() => setMembers(members.filter((_, j) => j !== i))}>Remove</button>
            </li>
          ))}
        </ul>
      )}
      <div className="space-x-2">
        <button className="btn btn-primary">Save team</button>
        <button type="button" className="field" onClick={onCancel}>Cancel</button>
      </div>
    </form>
  );
}
