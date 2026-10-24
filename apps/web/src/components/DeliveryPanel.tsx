"use client";

import { useState } from "react";
import { api, Commitment, Milestone, ProjectSummary, ScopeChange } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date, daysBetween, shortDate } from "@/lib/format";
import { can, useMe } from "./Shell";
import { Badge, Card, DeliveryDelta, Empty, ErrorBox, Table } from "./ui";

const STATUS_TEXT: Record<string, string> = {
  on_track: "Forecast meets the baseline",
  at_risk: "Forecast is after the baseline",
  late: "Forecast is after the baseline",
  tbd: "Forecast or baseline is TBD",
  delivered: "Delivered",
};

const COMMIT_TONE: Record<Commitment["status"], "ok" | "warn" | "bad" | "neutral"> = {
  approved: "ok", delivered: "ok", proposed: "warn", rejected: "bad", superseded: "neutral",
};

// Baseline, forecast and actual are shown side by side and never merged.
export function DeliverySummary({ project, today }: { project: ProjectSummary; today: string }) {
  const d = project.delivery;
  if (!d) return null;
  return (
    <Card title="Delivery" action={<span className="flex items-center gap-2 text-sm text-slate-600">{STATUS_TEXT[d.status] ?? d.status}<DeliveryDelta d={d} /></span>}>
      <Timeline today={today} baseline={d.baseline} forecast={d.forecast} actual={d.actual} />
      {(d.covers_project_ids?.length ?? 0) > 1 && (
        <p className="mb-2 text-sm text-slate-600">Planned together with its {d.covers_project_ids!.length - 1} subproject{d.covers_project_ids!.length === 2 ? "" : "s"}: one baseline, one forecast and their combined capacity.</p>
      )}
      <div className={`mt-4 grid gap-3 ${d.dev_done ? "md:grid-cols-4" : "md:grid-cols-3"}`}>
        <DateBox label="Approved baseline" value={d.baseline} note={d.baseline_source ?? "No approved commitment"} />
        {d.dev_done && <DateBox label="Development done" value={d.dev_done} note={`then ${d.inputs.qa_days ?? "?"} working days of QA`} />}
        <DateBox label={d.dev_done ? "Forecast release" : "Forecast"} value={d.forecast} note={`${d.method.replaceAll("_", " ")} · confidence ${d.confidence}`}
          tone={d.status === "at_risk" || d.status === "late" ? "text-amber-700" : undefined} />
        <DateBox label="Actual" value={d.actual} note={d.actual ? "Recorded" : "Not delivered"} />
      </div>
      {d.scope && <ScopeLine scope={d.scope} />}
      <details className="mt-3 text-sm">
        <summary className="cursor-pointer text-slate-600 hover:text-slate-900">How this forecast was made</summary>
        <ul className="mt-2 list-disc pl-5 text-slate-600">{d.assumptions.map((a) => <li key={a}>{a}</li>)}</ul>
        <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-slate-600 md:grid-cols-4">
          {Object.entries(d.inputs).map(([k, v]) => (
            <div key={k}><dt className="text-xs text-slate-500">{k.replaceAll("_", " ")}</dt><dd>{v === null ? "unknown" : String(v)}</dd></div>
          ))}
        </dl>
      </details>
    </Card>
  );
}

// ScopeLine shows how the work changed since the baseline froze its scope.
function ScopeLine({ scope: s }: { scope: ScopeChange }) {
  const hours = (h: number) => `${h > 0 ? "+" : ""}${Math.round(h * 10) / 10} h`;
  const changed = s.added.length > 0 || s.removed.length > 0 || Math.abs(s.estimate_change_hours) >= 0.1;
  return (
    <p className={`mt-3 rounded-lg px-3 py-2 text-sm ${changed ? "bg-amber-50 text-amber-900" : "bg-slate-50 text-slate-600"}`}>
      {changed ? (
        <>
          Scope since the baseline: {s.added.length} issue{s.added.length === 1 ? "" : "s"} added, {s.removed.length} removed
          {Math.abs(s.estimate_change_hours) >= 0.1 && <>, estimates {hours(s.estimate_change_hours)} on the original issues</>}.
          {" "}Now {s.current_issues} issues and {Math.round(s.current_hours)} h estimated, against {s.baseline_issues} issues and {Math.round(s.baseline_hours)} h at approval.
        </>
      ) : <>Scope unchanged since the baseline was approved ({s.baseline_issues} issues, {Math.round(s.baseline_hours)} h estimated).</>}
    </p>
  );
}

// A line from today to the latest date, with the dates that are known on it.
function Timeline({ today, baseline, forecast, actual }: { today: string; baseline: string | null; forecast: string | null; actual: string | null }) {
  const points = [
    { key: "today", label: "Today", v: today, mark: "h-3 w-3 rounded-full bg-slate-800" },
    { key: "baseline", label: "Baseline", v: baseline, mark: "h-3.5 w-3.5 rotate-45 bg-blue-700" },
    { key: "forecast", label: "Forecast", v: forecast, mark: "h-3.5 w-3.5 rounded-full bg-amber-500" },
    { key: "actual", label: "Actual", v: actual, mark: "h-3.5 w-3.5 rounded-sm bg-emerald-600" },
  ].filter((p): p is typeof p & { v: string } => !!p.v);
  if (points.length < 2) {
    return <p className="rounded-lg bg-slate-50 px-3 py-2 text-sm text-slate-500">The timeline appears once a baseline is approved or a forecast exists.</p>;
  }
  const days = points.map((p) => daysBetween(today, p.v));
  const lo = Math.min(...days), hi = Math.max(...days);
  const span = Math.max(hi - lo, 1);
  const x = (n: number) => 6 + ((n - lo) / span) * 88;
  const slip = baseline && forecast && daysBetween(baseline, forecast) > 0
    ? { from: x(daysBetween(today, baseline)), to: x(daysBetween(today, forecast)) } : null;
  return (
    <div className="relative h-16" aria-label="Delivery timeline" role="img">
      <div className="absolute inset-x-0 top-[13px] h-1 rounded-full bg-slate-200" />
      {slip && <div className="absolute top-[13px] h-1 bg-amber-500" style={{ left: `${slip.from}%`, width: `${slip.to - slip.from}%` }} />}
      {points.map((p, i) => (
        <div key={p.key} className="absolute top-2 flex -translate-x-1/2 flex-col items-center gap-2" style={{ left: `${x(days[i])}%` }}>
          <span className={p.mark} aria-hidden />
          <span className="whitespace-nowrap text-xs text-slate-600">{p.label} {shortDate(p.v, today)}</span>
        </div>
      ))}
    </div>
  );
}

function DateBox({ label, value, note, tone }: { label: string; value: string | null; note: string; tone?: string }) {
  return (
    <div className="rounded-lg border border-slate-200 p-3">
      <div className="text-xs font-medium text-slate-500">{label}</div>
      <div className={`mt-1 text-lg font-bold ${tone ?? "text-slate-900"}`}>{date(value)}</div>
      <div className="text-xs text-slate-500">{note}</div>
    </div>
  );
}

export function Commitments({ project, milestones, onChange }: { project: ProjectSummary; milestones: Milestone[]; onChange: () => void }) {
  const me = useMe();
  const list = useApi<{ commitments: Commitment[] }>(`/commitments?project=${project.id}`);
  const [err, setErr] = useState<Error | null>(null);
  const refresh = () => { list.reload(); onChange(); };
  async function act(fn: () => Promise<unknown>) {
    setErr(null);
    try { await fn(); refresh(); } catch (e) { setErr(e as Error); }
  }
  const versionName = (id: number | null) => (id === null ? "Whole project" : milestones.find((m) => m.id === id)?.name ?? `Version ${id}`);

  return (
    <Card title="Commitments">
      <p className="-mt-1 mb-3 text-sm text-slate-500">A baseline is a date someone committed to. It only counts once it is approved, and it never moves with the forecast.</p>
      <ErrorBox error={err} />
      <Table head={["Scope", "Title", "Baseline", "Status", "Reason", "Proposed", "Decided", ""]}>
        {(list.data?.commitments ?? []).map((c) => (
          <tr key={c.id}>
            <td className="px-2 py-2.5">{versionName(c.version_id)}</td>
            <td className="px-2 py-2.5 font-medium">{c.title}</td>
            <td className="whitespace-nowrap px-2 py-2.5">{date(c.baseline_date)}{c.actual_date && <> · actual {date(c.actual_date)}</>}</td>
            <td className="px-2 py-2.5"><Badge tone={COMMIT_TONE[c.status]}>{c.status}</Badge>{c.supersedes_id ? <span className="ml-1 text-xs text-slate-500">(revision)</span> : ""}</td>
            <td className="px-2 py-2.5 text-slate-600">{c.reason}</td>
            <td className="px-2 py-2.5 text-slate-600">{c.proposed_by}, {date(c.proposed_at)}</td>
            <td className="px-2 py-2.5 text-slate-600">{c.approved_by ? `${c.approved_by}, ${date(c.approved_at)}` : ""}</td>
            <td className="space-x-1 whitespace-nowrap px-2 py-2.5">
              {c.status === "proposed" && can(me, "approve_commitments") && (
                <>
                  <button className="btn btn-primary btn-sm" onClick={() => act(() => api(`/commitments/${c.id}/decision`, { method: "POST", body: { approve: true, note: "" } }))}>Approve</button>
                  <button className="btn btn-sm" onClick={() => {
                    const note = prompt("Why is this rejected?");
                    if (note) act(() => api(`/commitments/${c.id}/decision`, { method: "POST", body: { approve: false, note } }));
                  }}>Reject</button>
                </>
              )}
              {c.status === "approved" && can(me, "propose_commitments") && (
                <button className="btn btn-sm" onClick={() => {
                  const actual = prompt("Actual delivery date (YYYY-MM-DD)");
                  if (actual) act(() => api(`/commitments/${c.id}/delivered`, { method: "POST", body: { actual_date: actual } }));
                }}>Mark delivered</button>
              )}
            </td>
          </tr>
        ))}
      </Table>
      {list.data?.commitments.length === 0 && <Empty>No commitments recorded. The baseline stays TBD until one is approved.</Empty>}
      {can(me, "propose_commitments") && <ProposeForm projectId={project.id} milestones={milestones} onDone={refresh} />}
    </Card>
  );
}

type Snapshot = { date: string; health: string; forecast: string | null; baseline: string | null; open: number; confidence: string };

export function RiskHistory({ projectId }: { projectId: number }) {
  const hist = useApi<{ snapshots: Snapshot[] }>(`/projects/${projectId}/history?days=90`);
  return (
    <Card title="Risk history · last 90 days">
      <ErrorBox error={hist.error} />
      <Table head={["Date", "Health", "Baseline", "Forecast", "Confidence", "Open"]}>
        {(hist.data?.snapshots ?? []).slice().reverse().map((s) => (
          <tr key={s.date}>
            <td className="px-2 py-2">{date(s.date)}</td><td className="px-2 py-2">{s.health.replaceAll("_", " ")}</td>
            <td className="px-2 py-2">{date(s.baseline)}</td><td className="px-2 py-2">{date(s.forecast)}</td>
            <td className="px-2 py-2">{s.confidence}</td><td className="px-2 py-2 tabular-nums">{s.open}</td>
          </tr>
        ))}
      </Table>
      {hist.data?.snapshots.length === 0 && <Empty>No daily snapshots yet.</Empty>}
    </Card>
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
    <form onSubmit={submit} className="mt-4 rounded-lg border border-dashed border-slate-300 bg-slate-50 p-3">
      <div className="mb-2 text-sm font-medium text-slate-700">Propose a baseline</div>
      <div className="flex flex-wrap items-end gap-2">
        <select aria-label="Scope" value={f.version} onChange={(e) => setF({ ...f, version: e.target.value })} className="field">
          <option value="">Whole project</option>
          {milestones.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}
        </select>
        <input required placeholder="Title" value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} className="field" />
        <input required type="date" aria-label="Baseline date" value={f.baseline_date} onChange={(e) => setF({ ...f, baseline_date: e.target.value })} className="field" />
        <input required placeholder="Reason or source of the commitment" value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })} className="field min-w-64 flex-1" />
        <button className="btn btn-primary">Propose baseline</button>
      </div>
      <div className="mt-2"><ErrorBox error={err} /></div>
    </form>
  );
}
