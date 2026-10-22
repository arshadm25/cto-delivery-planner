"use client";

import { useState } from "react";
import { api, Commitment, Milestone, ProjectSummary } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date } from "@/lib/format";
import { can, useMe } from "./Shell";
import { Card, ErrorBox, Table } from "./ui";

const STATUS_TEXT: Record<string, string> = {
  on_track: "Forecast meets the baseline",
  at_risk: "Forecast is after the baseline",
  tbd: "Forecast or baseline is TBD",
  delivered: "Delivered",
};

// Baseline, forecast and actual are shown side by side and never merged.
export function DeliveryPanel({ project, milestones, onChange }: { project: ProjectSummary; milestones: Milestone[]; onChange: () => void }) {
  const me = useMe();
  const d = project.delivery;
  const list = useApi<{ commitments: Commitment[] }>(`/commitments?project=${project.id}`);
  const hist = useApi<{ snapshots: { date: string; health: string; forecast: string | null; baseline: string | null; open: number; confidence: string }[] }>(`/projects/${project.id}/history?days=90`);
  const [err, setErr] = useState<Error | null>(null);
  const refresh = () => { list.reload(); onChange(); };
  async function act(fn: () => Promise<unknown>) {
    setErr(null);
    try { await fn(); refresh(); } catch (e) { setErr(e as Error); }
  }
  const versionName = (id: number | null) => (id === null ? "Whole project" : milestones.find((m) => m.id === id)?.name ?? `Version ${id}`);

  return (
    <Card title="Delivery">
      {d && (
        <div className="grid gap-4 md:grid-cols-3">
          <DateBox label="Approved baseline" value={d.baseline} note={d.baseline_source ?? "No approved commitment"} />
          <DateBox label="Forecast" value={d.forecast} note={`${d.method.replaceAll("_", " ")} · confidence ${d.confidence}`} />
          <DateBox label="Actual" value={d.actual} note={d.actual ? "Recorded" : "Not delivered"} />
        </div>
      )}
      {d && (
        <div className="mt-3 text-sm">
          <p className={d.status === "at_risk" ? "font-medium text-amber-800" : "text-slate-700"}>{STATUS_TEXT[d.status] ?? d.status}</p>
          <details className="mt-2">
            <summary className="cursor-pointer text-slate-600">How this forecast was made</summary>
            <ul className="mt-2 list-disc pl-5 text-slate-600">{d.assumptions.map((a) => <li key={a}>{a}</li>)}</ul>
            <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-slate-600 md:grid-cols-4">
              {Object.entries(d.inputs).map(([k, v]) => (
                <div key={k}><dt className="text-xs text-slate-500">{k.replaceAll("_", " ")}</dt><dd>{v === null ? "unknown" : String(v)}</dd></div>
              ))}
            </dl>
          </details>
        </div>
      )}

      <ErrorBox error={err} />
      <h3 className="mb-2 mt-5 text-sm font-semibold text-slate-700">Commitments</h3>
      <Table head={["Scope", "Title", "Baseline", "Status", "Reason", "Proposed", "Decided", ""]}>
        {(list.data?.commitments ?? []).map((c) => (
          <tr key={c.id}>
            <td className="px-2 py-2">{versionName(c.version_id)}</td>
            <td className="px-2 py-2">{c.title}</td>
            <td className="px-2 py-2">{date(c.baseline_date)}{c.actual_date && <> · actual {date(c.actual_date)}</>}</td>
            <td className="px-2 py-2">{c.status}{c.supersedes_id ? " (revision)" : ""}</td>
            <td className="px-2 py-2 text-slate-600">{c.reason}</td>
            <td className="px-2 py-2 text-slate-600">{c.proposed_by}, {date(c.proposed_at)}</td>
            <td className="px-2 py-2 text-slate-600">{c.approved_by ? `${c.approved_by}, ${date(c.approved_at)}` : ""}</td>
            <td className="px-2 py-2 space-x-1 whitespace-nowrap">
              {c.status === "proposed" && can(me, "approve_commitments") && (
                <>
                  <button className="rounded bg-slate-900 px-2 py-0.5 text-white" onClick={() => act(() => api(`/commitments/${c.id}/decision`, { method: "POST", body: { approve: true, note: "" } }))}>Approve</button>
                  <button className="rounded border px-2 py-0.5" onClick={() => {
                    const note = prompt("Why is this rejected?");
                    if (note) act(() => api(`/commitments/${c.id}/decision`, { method: "POST", body: { approve: false, note } }));
                  }}>Reject</button>
                </>
              )}
              {c.status === "approved" && can(me, "propose_commitments") && (
                <button className="rounded border px-2 py-0.5" onClick={() => {
                  const actual = prompt("Actual delivery date (YYYY-MM-DD)");
                  if (actual) act(() => api(`/commitments/${c.id}/delivered`, { method: "POST", body: { actual_date: actual } }));
                }}>Mark delivered</button>
              )}
            </td>
          </tr>
        ))}
      </Table>
      {list.data?.commitments.length === 0 && <p className="text-sm text-slate-500">No commitments recorded. The baseline stays TBD until one is approved.</p>}
      {can(me, "propose_commitments") && <ProposeForm projectId={project.id} milestones={milestones} onDone={refresh} />}

      {(hist.data?.snapshots.length ?? 0) > 0 && (
        <details className="mt-5 text-sm">
          <summary className="cursor-pointer font-semibold text-slate-700">Risk history (last 90 days)</summary>
          <Table head={["Date", "Health", "Baseline", "Forecast", "Confidence", "Open"]}>
            {hist.data!.snapshots.slice().reverse().map((s) => (
              <tr key={s.date}><td className="px-2 py-1">{date(s.date)}</td><td className="px-2 py-1">{s.health.replaceAll("_", " ")}</td>
                <td className="px-2 py-1">{date(s.baseline)}</td><td className="px-2 py-1">{date(s.forecast)}</td>
                <td className="px-2 py-1">{s.confidence}</td><td className="px-2 py-1">{s.open}</td></tr>
            ))}
          </Table>
        </details>
      )}
    </Card>
  );
}

function DateBox({ label, value, note }: { label: string; value: string | null; note: string }) {
  return (
    <div className="rounded border border-slate-200 p-3">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-lg font-semibold">{date(value)}</div>
      <div className="text-xs text-slate-500">{note}</div>
    </div>
  );
}

function ProposeForm({ projectId, milestones, onDone }: { projectId: number; milestones: Milestone[]; onDone: () => void }) {
  const [f, setF] = useState({ version: "", title: "", baseline_date: "", reason: "" });
  const [err, setErr] = useState<Error | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    try {
      await api("/commitments", { method: "POST", body: {
        project_id: projectId, version_id: f.version ? Number(f.version) : null, title: f.title, baseline_date: f.baseline_date, reason: f.reason,
      } });
      setF({ version: "", title: "", baseline_date: "", reason: "" });
      onDone();
    } catch (e) { setErr(e as Error); }
  }
  return (
    <form onSubmit={submit} className="mt-3 flex flex-wrap items-end gap-2 text-sm">
      <select aria-label="Scope" value={f.version} onChange={(e) => setF({ ...f, version: e.target.value })} className="rounded border px-2 py-1">
        <option value="">Whole project</option>
        {milestones.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
      </select>
      <input required placeholder="Title" value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} className="rounded border px-2 py-1" />
      <input required type="date" aria-label="Baseline date" value={f.baseline_date} onChange={(e) => setF({ ...f, baseline_date: e.target.value })} className="rounded border px-2 py-1" />
      <input required placeholder="Reason or source of the commitment" value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })} className="min-w-64 rounded border px-2 py-1" />
      <button className="rounded border border-slate-900 px-2 py-1">Propose baseline</button>
      <ErrorBox error={err} />
    </form>
  );
}
