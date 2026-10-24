"use client";
import { useState } from "react";
import { deleteMilestone, Milestone, saveMilestone } from "@/lib/api";
import { date, hours } from "@/lib/format";
import { can, useMe } from "./Shell";
import { Card, ErrorBox, FlagChip, Table } from "./ui";

// Milestones of a project: Redmine versions plus milestones created in the
// planner. Only planner milestones can be edited here; Redmine is never
// changed. Issues are put into milestones from the Issues card.

export function MilestonesCard({ projectId, milestones, error, onChange }: {
  projectId: number; milestones: Milestone[]; error?: Error | null; onChange: () => void;
}) {
  const me = useMe();
  const editable = can(me, "plan_work");
  const [editing, setEditing] = useState<number | null>(null);
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

  const head = ["Milestone", "Due", "Status", "Open / total", "Remaining estimate", "Baseline", "Flags"];
  return (
    <Card title="Milestones">
      <ErrorBox error={err ?? error ?? null} />
      <Table head={editable ? [...head, "Edit"] : head}>
        {milestones.map((m) => editing === m.id ? (
          <tr key={m.id} className="bg-amber-50">
            <td colSpan={head.length + 1} className="px-2 py-2">
              <MilestoneForm initial={m} submitLabel="Save" onCancel={() => setEditing(null)}
                onSubmit={(b) => act(() => saveMilestone(projectId, m.id, b))} />
            </td>
          </tr>
        ) : (
          <tr key={m.id}>
            <td className="px-2 py-2">
              {m.redmine_url ? <a className="hover:underline" href={m.redmine_url} target="_blank" rel="noreferrer">{m.name}</a> : m.name}
              {m.planner && <span className="ml-1 rounded bg-violet-100 px-1 text-[10px] font-medium uppercase text-violet-800">planner</span>}
            </td>
            <td className="px-2 py-2">{date(m.due_date)}</td>
            <td className="px-2 py-2">{m.status}</td>
            <td className="px-2 py-2">{m.open} / {m.total}</td>
            <td className="px-2 py-2">{hours(m.remaining_estimated_hours)}{m.open_no_estimate ? ` (+${m.open_no_estimate} unestimated)` : ""}</td>
            <td className="px-2 py-2">{m.commitment?.status === "approved" ? date(m.commitment.baseline_date) : "TBD"}</td>
            <td className="px-2 py-2 space-x-1">{m.flags.map((f, i) => <FlagChip key={i} kind={f.kind} />)}</td>
            {editable && (
              <td className="px-2 py-2 space-x-1 whitespace-nowrap">
                {m.planner ? (
                  <>
                    <button className="btn btn-sm" onClick={() => setEditing(m.id)}>Edit</button>
                    <button className="btn btn-sm" onClick={() => {
                      if (confirm(`Delete “${m.name}”? Its issues go back to their Redmine milestone.`)) act(() => deleteMilestone(projectId, m.id));
                    }}>Delete</button>
                  </>
                ) : <span className="text-xs text-slate-400">from Redmine</span>}
              </td>
            )}
          </tr>
        ))}
      </Table>
      {milestones.length === 0 && <p className="text-sm text-slate-500">No milestones yet.</p>}
      {editable && (
        <div className="mt-3">
          <h3 className="mb-1 text-sm font-semibold text-slate-700">Add a milestone</h3>
          <MilestoneForm submitLabel="Add milestone" onSubmit={(b) => act(() => saveMilestone(projectId, null, b))} />
        </div>
      )}
    </Card>
  );
}

function MilestoneForm({ initial, submitLabel, onSubmit, onCancel }: {
  initial?: Milestone; submitLabel: string; onSubmit: (b: { name: string; due_date: string | null; status: "open" | "closed" }) => Promise<void> | void; onCancel?: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [due, setDue] = useState(initial?.due_date?.slice(0, 10) ?? "");
  const [status, setStatus] = useState<"open" | "closed">(initial?.status === "closed" ? "closed" : "open");
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    await onSubmit({ name, due_date: due || null, status });
    if (!initial) { setName(""); setDue(""); }
  }
  return (
    <form onSubmit={submit} className="flex flex-wrap items-end gap-2 text-sm">
      <input required aria-label="Milestone name" placeholder="Name, e.g. Phase 2 or UAT" value={name} onChange={(e) => setName(e.target.value)} className="field min-w-56" />
      <input type="date" aria-label="Milestone due date" value={due} onChange={(e) => setDue(e.target.value)} className="field" />
      {initial && (
        <select aria-label="Milestone status" value={status} onChange={(e) => setStatus(e.target.value as "open" | "closed")} className="field">
          <option value="open">open</option><option value="closed">closed</option>
        </select>
      )}
      <button className="btn btn-primary">{submitLabel}</button>
      {onCancel && <button type="button" className="field" onClick={onCancel}>Cancel</button>}
    </form>
  );
}
