"use client";
import { useState } from "react";
import { FLAG_LABEL, Issue, Milestone, Person, PlanField, planIssues } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date, hours } from "@/lib/format";
import { can, useMe } from "./Shell";
import { Card, ErrorBox, FlagChip, Pager, Table } from "./ui";

// Issues of a project, with planning done in the planner: estimate, due
// date, assignee and milestone can be set for one issue or many at once.
// Planned values replace Redmine's in every view; Redmine is never changed.

type Changes = Partial<Record<PlanField, string | number | null>>;

const FLAG_FILTERS = Object.entries(FLAG_LABEL).filter(
  ([k]) => !k.startsWith("milestone") && !["forecast_after_baseline", "capacity_risk", "no_baseline", "release_readiness"].includes(k),
);

export function IssuesCard({ projectId, milestones, onChange }: { projectId: number; milestones: Milestone[]; onChange: () => void }) {
  const me = useMe();
  const editable = can(me, "plan_work");
  const [status, setStatus] = useState("open");
  const [flag, setFlag] = useState("");
  const [version, setVersion] = useState("");
  const [q, setQ] = useState("");
  const [sort, setSort] = useState("id");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [editing, setEditing] = useState<number | null>(null);
  const [err, setErr] = useState<Error | null>(null);
  const people = useApi<{ people: Person[] }>(editable ? "/people" : null);

  const qs = new URLSearchParams({ status, page: String(page), per_page: "50", sort });
  if (flag) qs.set("flag", flag);
  if (version) qs.set("version", version);
  if (q) qs.set("q", q);
  const { data, error, reload } = useApi<{ issues: Issue[]; total: number; page: number; per_page: number }>(`/projects/${projectId}/issues?${qs}`);
  const resetPage = () => { setPage(1); setSelected(new Set()); };

  async function apply(ids: number[], changes: Changes) {
    setErr(null);
    try {
      await planIssues(projectId, ids, changes);
      setEditing(null);
      setSelected(new Set());
      reload();
      onChange();
    } catch (e) {
      setErr(e as Error);
    }
  }

  const rows = data?.issues ?? [];
  const allOnPage = rows.length > 0 && rows.every((r) => selected.has(r.id));
  const toggle = (id: number) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id); else next.add(id);
    setSelected(next);
  };
  const head = ["#", "Subject", "Status", "Assignee", "Milestone", "Due", "Estimate", "Spent", "Flags"];

  return (
    <Card title="Issues">
      <div className="mb-3 flex flex-wrap gap-2 text-sm">
        <select aria-label="Status" value={status} onChange={(e) => { setStatus(e.target.value); resetPage(); }} className="field">
          <option value="open">Open</option><option value="awaiting_qa">Awaiting QA</option><option value="closed">Closed</option><option value="all">All</option>
        </select>
        <select aria-label="Flag" value={flag} onChange={(e) => { setFlag(e.target.value); resetPage(); }} className="field">
          <option value="">Any flag</option>
          {FLAG_FILTERS.map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <select aria-label="Milestone filter" value={version} onChange={(e) => { setVersion(e.target.value); resetPage(); }} className="field">
          <option value="">Any milestone</option>
          {milestones.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
        <select aria-label="Sort" value={sort} onChange={(e) => setSort(e.target.value)} className="field">
          <option value="id">By ID</option><option value="due">By due date</option><option value="updated">Recently updated</option>
        </select>
        <input aria-label="Search" placeholder="Search subject or #ID" value={q} onChange={(e) => { setQ(e.target.value); resetPage(); }} className="field" />
      </div>
      {editable && selected.size > 0 && (
        <BulkBar count={selected.size} milestones={milestones} people={people.data?.people ?? []}
          onApply={(c) => apply([...selected], c)} onClear={() => setSelected(new Set())} />
      )}
      <ErrorBox error={err ?? error} />
      {editable && (
        <label className="mb-1 flex items-center gap-2 text-xs text-slate-500">
          <input type="checkbox" checked={allOnPage} onChange={() => setSelected(allOnPage ? new Set() : new Set(rows.map((r) => r.id)))} />
          Select all on this page
        </label>
      )}
      <Table head={editable ? ["Pick", ...head, "Plan"] : head}>
        {rows.map((is) => editing === is.id ? (
          <EditRow key={is.id} issue={is} milestones={milestones} people={people.data?.people ?? []}
            onSave={(c) => apply([is.id], c)} onCancel={() => setEditing(null)} />
        ) : (
          <tr key={is.id} className={selected.has(is.id) ? "bg-blue-50" : undefined}>
            {editable && <td className="px-2 py-2"><input type="checkbox" aria-label={`Select issue ${is.id}`} checked={selected.has(is.id)} onChange={() => toggle(is.id)} /></td>}
            <td className="px-2 py-2">{is.redmine_url ? <a className="link" href={is.redmine_url} target="_blank" rel="noreferrer">{is.id}</a> : is.id}</td>
            <td className="px-2 py-2">{is.subject}</td>
            <td className="px-2 py-2">{is.status}</td>
            <td className="px-2 py-2"><Planned issue={is} field="assignee_id">{is.assignee_name ?? "TBD"}</Planned></td>
            <td className="px-2 py-2"><Planned issue={is} field="version_id" milestones={milestones}>{is.version_name ?? "None"}</Planned></td>
            <td className="px-2 py-2"><Planned issue={is} field="due_date">{date(is.due_date)}</Planned></td>
            <td className="px-2 py-2"><Planned issue={is} field="estimated_hours">{hours(is.estimated_hours)}</Planned></td>
            <td className="px-2 py-2">{hours(is.spent_hours)}</td>
            <td className="px-2 py-2 space-x-1">{is.flags.map((f) => <FlagChip key={f} kind={f} />)}</td>
            {editable && <td className="px-2 py-2"><button className="btn btn-sm" onClick={() => setEditing(is.id)}>Edit</button></td>}
          </tr>
        ))}
      </Table>
      {data && <Pager page={data.page} perPage={data.per_page} total={data.total} onPage={(p) => { setPage(p); setSelected(new Set()); }} />}
      {editable && <p className="mt-2 text-xs text-slate-500">Values marked “planner” were set here and replace Redmine’s. Redmine itself is not changed.</p>}
    </Card>
  );
}

// Planned marks a value set in the planner and shows Redmine's value on hover.
function Planned({ issue, field, milestones, children }: { issue: Issue; field: PlanField; milestones?: Milestone[]; children: React.ReactNode }) {
  if (!issue.planned?.includes(field)) return <>{children}</>;
  let was = issue.redmine_values?.[field];
  if (field === "version_id" && was != null) was = milestones?.find((m) => m.id === was)?.name ?? `Version ${was}`;
  if (field === "assignee_id" && was != null) was = `user #${was}`;
  return (
    <span title={`Redmine: ${was ?? "not set"}`}>
      {children} <span className="rounded bg-violet-100 px-1 text-[10px] font-medium uppercase text-violet-800">planner</span>
    </span>
  );
}

function EditRow({ issue, milestones, people, onSave, onCancel }: {
  issue: Issue; milestones: Milestone[]; people: Person[]; onSave: (c: Changes) => void; onCancel: () => void;
}) {
  const [f, setF] = useState({
    estimated_hours: issue.estimated_hours === null ? "" : String(issue.estimated_hours),
    due_date: issue.due_date?.slice(0, 10) ?? "",
    assignee_id: issue.assignee_id === null ? "" : String(issue.assignee_id),
    version_id: issue.version_id === null ? "" : String(issue.version_id),
  });
  const planned = new Set(issue.planned ?? []);

  function save() {
    // Only send fields that changed. An emptied field goes back to Redmine.
    const c: Changes = {};
    const was = {
      estimated_hours: issue.estimated_hours === null ? "" : String(issue.estimated_hours),
      due_date: issue.due_date?.slice(0, 10) ?? "",
      assignee_id: issue.assignee_id === null ? "" : String(issue.assignee_id),
      version_id: issue.version_id === null ? "" : String(issue.version_id),
    };
    if (f.estimated_hours !== was.estimated_hours) c.estimated_hours = f.estimated_hours === "" ? null : Number(f.estimated_hours);
    if (f.due_date !== was.due_date) c.due_date = f.due_date || null;
    if (f.assignee_id !== was.assignee_id) c.assignee_id = f.assignee_id === "" ? null : Number(f.assignee_id);
    if (f.version_id !== was.version_id) c.version_id = f.version_id === "" ? null : Number(f.version_id);
    if (Object.keys(c).length === 0) onCancel(); else onSave(c);
  }
  const revert = (field: PlanField) => onSave({ [field]: null });
  const RevertBtn = ({ field }: { field: PlanField }) =>
    planned.has(field) ? <button type="button" title="Go back to the Redmine value" className="link ml-1 text-xs" onClick={() => revert(field)}>use Redmine</button> : null;

  return (
    <tr className="bg-amber-50">
      <td className="px-2 py-2" />
      <td className="px-2 py-2">{issue.id}</td>
      <td className="px-2 py-2">{issue.subject}</td>
      <td className="px-2 py-2">{issue.status}</td>
      <td className="px-2 py-2">
        <select aria-label="Assignee" value={f.assignee_id} onChange={(e) => setF({ ...f, assignee_id: e.target.value })} className="field max-w-40">
          <option value="">Unassigned</option>
          {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select><RevertBtn field="assignee_id" />
      </td>
      <td className="px-2 py-2">
        <select aria-label="Milestone" value={f.version_id} onChange={(e) => setF({ ...f, version_id: e.target.value })} className="field max-w-40">
          <option value="">None</option>
          {milestones.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select><RevertBtn field="version_id" />
      </td>
      <td className="px-2 py-2">
        <input type="date" aria-label="Due date" value={f.due_date} onChange={(e) => setF({ ...f, due_date: e.target.value })} className="field px-2 py-1" />
        <RevertBtn field="due_date" />
      </td>
      <td className="px-2 py-2">
        <input type="number" min={0} step="0.5" aria-label="Estimate hours" value={f.estimated_hours} onChange={(e) => setF({ ...f, estimated_hours: e.target.value })} className="field w-20" />
        <RevertBtn field="estimated_hours" />
      </td>
      <td className="px-2 py-2">{hours(issue.spent_hours)}</td>
      <td className="px-2 py-2" />
      <td className="px-2 py-2 space-x-1 whitespace-nowrap">
        <button className="btn btn-primary btn-sm" onClick={save}>Save</button>
        <button className="btn btn-sm" onClick={onCancel}>Cancel</button>
      </td>
    </tr>
  );
}

// BulkBar sets one value on every selected issue.
function BulkBar({ count, milestones, people, onApply, onClear }: {
  count: number; milestones: Milestone[]; people: Person[]; onApply: (c: Changes) => void; onClear: () => void;
}) {
  const [field, setField] = useState<PlanField>("version_id");
  const [value, setValue] = useState("");
  function apply() {
    if (field === "estimated_hours" || field === "assignee_id" || field === "version_id") {
      onApply({ [field]: value === "" ? null : Number(value) });
    } else {
      onApply({ [field]: value || null });
    }
    setValue("");
  }
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-blue-200 bg-blue-50 p-2 text-sm">
      <span className="font-medium">{count} selected</span>
      <span>Set</span>
      <select aria-label="Field to set" value={field} onChange={(e) => { setField(e.target.value as PlanField); setValue(""); }} className="field">
        <option value="version_id">Milestone</option>
        <option value="assignee_id">Assignee</option>
        <option value="due_date">Due date</option>
        <option value="estimated_hours">Estimate</option>
      </select>
      <span>to</span>
      {field === "version_id" && (
        <select aria-label="Bulk milestone" value={value} onChange={(e) => setValue(e.target.value)} className="field">
          <option value="">Redmine value</option>
          {milestones.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
      )}
      {field === "assignee_id" && (
        <select aria-label="Bulk assignee" value={value} onChange={(e) => setValue(e.target.value)} className="field">
          <option value="">Redmine value</option>
          {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      )}
      {field === "due_date" && <input type="date" aria-label="Bulk due date" value={value} onChange={(e) => setValue(e.target.value)} className="field" />}
      {field === "estimated_hours" && <input type="number" min={0} step="0.5" aria-label="Bulk estimate" placeholder="hours" value={value} onChange={(e) => setValue(e.target.value)} className="field w-24" />}
      <button className="btn btn-primary" onClick={apply}>Apply to {count}</button>
      <button className="field" onClick={onClear}>Clear selection</button>
      <span className="text-xs text-slate-500">Leaving the value empty goes back to Redmine’s value.</span>
    </div>
  );
}
