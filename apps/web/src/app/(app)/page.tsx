"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { HEALTH_LABEL, HealthState, Portfolio, ProjectSummary } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { ago, daysBetween, hours, pct, relDays, shortDate } from "@/lib/format";
import { Card, DeliveryDelta, Empty, ErrorBox, HEALTH_COLOR, HealthBadge, Loading, PageHeader, Progress, StaleBanner, Stat } from "@/components/ui";

const ORDER: HealthState[] = ["blocked", "at_risk", "insufficient_data", "on_track"];

const byHealth = (a: ProjectSummary, b: ProjectSummary) =>
  ORDER.indexOf(a.health.state) - ORDER.indexOf(b.health.state) || a.name.localeCompare(b.name);

const slipping = (p: ProjectSummary) => p.delivery?.status === "at_risk" || p.delivery?.status === "late";

interface Row { p: ProjectSummary; depth: number; children: number }

export default function Overview() {
  const { data, error, loading } = useApi<Portfolio>("/portfolio/summary");
  const [filter, setFilter] = useState<HealthState | "">("");
  const [q, setQ] = useState("");
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set());

  const rows = useMemo<Row[]>(() => {
    if (!data) return [];
    const visible = new Map(data.projects.map((p) => [p.id, p]));
    // Filtering shows a flat list; otherwise subprojects sit under their parent.
    if (filter || q) {
      const needle = q.trim().toLowerCase();
      return data.projects
        .filter((p) => (!filter || p.health.state === filter) && (!needle || p.name.toLowerCase().includes(needle)))
        .sort(byHealth)
        .map((p) => ({ p, depth: 0, children: 0 }));
    }
    const kids = new Map<number, ProjectSummary[]>();
    const roots: ProjectSummary[] = [];
    for (const p of data.projects) {
      if (p.parent_id !== null && visible.has(p.parent_id)) {
        kids.set(p.parent_id, [...(kids.get(p.parent_id) ?? []), p]);
      } else {
        roots.push(p);
      }
    }
    const out: Row[] = [];
    const walk = (list: ProjectSummary[], depth: number) => {
      for (const p of list.sort(byHealth)) {
        const children = kids.get(p.id) ?? [];
        out.push({ p, depth, children: children.length });
        if (!collapsed.has(p.id)) walk(children, depth + 1);
      }
    };
    walk(roots, 0);
    return out;
  }, [data, filter, q, collapsed]);

  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;

  const ps = data.projects;
  const names = new Map(ps.map((p) => [p.id, p.name]));
  const awaitingQA = ps.reduce((n, p) => n + p.issues.awaiting_qa, 0);
  const dev = ps.reduce((n, p) => n + p.issues.open - p.issues.awaiting_qa, 0);
  const devNoEstimate = ps.reduce((n, p) => n + p.issues.open_no_estimate, 0);
  const slips = ps.filter(slipping).length;
  const noBaseline = ps.filter((p) => !p.delivery?.baseline).length;
  const attention = ps.filter((p) => p.health.state === "blocked" || p.health.state === "at_risk" || slipping(p)).sort(byHealth);
  const total = ps.length || 1;

  const toggle = (id: number) => setCollapsed((s) => {
    const n = new Set(s);
    if (n.has(id)) n.delete(id); else n.add(id);
    return n;
  });

  return (
    <>
      <StaleBanner stale={data.data_stale} lastSync={data.last_successful_sync} />
      <div className="flex flex-wrap items-end justify-between gap-3">
        <PageHeader title="Portfolio overview" subtitle={`${data.projects_in_scope} projects in scope · ${shortDate(data.today, data.today)}`} />
        <div className="flex items-center gap-2">
          <Link href="/sync" className="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-600 shadow-xs hover:bg-slate-50">
            <span className={`h-2 w-2 rounded-full ${data.data_stale ? "bg-amber-500" : "bg-emerald-600"}`} aria-hidden />
            Synced {ago(data.last_successful_sync)}
          </Link>
        </div>
      </div>

      <Card title="Portfolio health" action={filter && <button className="link text-sm" onClick={() => setFilter("")}>Show all</button>}>
        <div className="flex h-3.5 gap-0.5 overflow-hidden rounded-full bg-slate-100">
          {ORDER.map((s) => (data.by_health[s] ?? 0) > 0 && (
            <button key={s} aria-label={`Show ${HEALTH_LABEL[s]} projects`} onClick={() => setFilter(filter === s ? "" : s)}
              className={`${HEALTH_COLOR[s].bar} transition hover:opacity-80 ${filter && filter !== s ? "opacity-30" : ""}`}
              style={{ width: `${((data.by_health[s] ?? 0) / total) * 100}%` }} />
          ))}
        </div>
        <div className="mt-3 flex flex-wrap gap-x-6 gap-y-2 text-sm">
          {ORDER.map((s) => (
            <button key={s} onClick={() => setFilter(filter === s ? "" : s)} aria-pressed={filter === s}
              className={`flex items-center gap-2 rounded px-1 ${filter === s ? "bg-slate-100 font-semibold" : ""}`}>
              <span className={`h-2.5 w-2.5 rounded-sm ${HEALTH_COLOR[s].bar}`} aria-hidden />
              <span className="font-semibold tabular-nums">{data.by_health[s] ?? 0}</span> {HEALTH_LABEL[s]}
            </button>
          ))}
        </div>
      </Card>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <Stat label="Forecast slipping" value={slips} tone={slips ? "warn" : "ok"} sub="projects past their baseline" />
        <Stat label="No approved baseline" value={noBaseline} sub={`of ${ps.length} projects`} />
        <Stat label="Open work in dev" value={dev.toLocaleString()} sub={`+${awaitingQA.toLocaleString()} awaiting QA`} />
        <Stat label="Overdue issues" value={data.overdue_issues.toLocaleString()} tone={data.overdue_issues ? "warn" : undefined} sub="past their due date" />
        <Stat label="Blocked issues" value={data.blocked_issues} tone={data.critical_blocked ? "bad" : undefined} sub={`${data.critical_blocked} critical`} />
        <Stat label="Estimate coverage" value={pct(dev ? (dev - devNoEstimate) / dev : null)} sub="of open dev issues" />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card title="Needs your attention" action={<Link className="link text-sm" href="/risks">All risks</Link>} className="lg:col-span-2">
          {attention.length === 0 ? <Empty>Nothing is blocked or slipping.</Empty> : (
            <ul className="-my-1 divide-y divide-slate-100">
              {attention.slice(0, 6).map((p) => (
                <li key={p.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 py-3">
                  <HealthBadge state={p.health.state} />
                  <div className="min-w-0 flex-1">
                    <div className="font-semibold text-slate-900">{p.name}</div>
                    <div className="text-sm text-slate-500">{p.health.reasons.join(" · ") || "Forecast is after the approved baseline"}</div>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-slate-600">
                    <span className="whitespace-nowrap">Forecast {shortDate(p.delivery?.forecast, data.today)} · Baseline {shortDate(p.delivery?.baseline, data.today)}</span>
                    <DeliveryDelta d={p.delivery} />
                  </div>
                  <Link className="link text-sm" href={`/projects/${p.id}`}>Review</Link>
                </li>
              ))}
            </ul>
          )}
          {attention.length > 6 && <p className="mt-2 text-sm text-slate-500">{attention.length - 6} more in the project list below.</p>}
        </Card>

        <div className="space-y-4">
          <Card title="Milestones · next 30 days">
            {data.upcoming_milestones.length === 0 ? <p className="text-sm text-slate-500">None with a date.</p> : (
              <ul className="space-y-4 text-sm">
                {data.upcoming_milestones.map((m) => {
                  const days = m.due_date ? daysBetween(data.today, m.due_date) : null;
                  return (
                    <li key={m.id} className="space-y-1.5">
                      <div className="flex items-baseline justify-between gap-2">
                        <Link href={`/projects/${m.project_id}`} className="link">{m.name}</Link>
                        <span className={`whitespace-nowrap text-xs font-semibold ${days !== null && days <= 7 ? "text-amber-700" : "text-slate-600"}`}>{relDays(m.due_date, data.today)}</span>
                      </div>
                      <Progress value={m.total ? (m.total - m.open) / m.total : 0} className="w-full" color="bg-blue-700" label={`${m.name} progress`} />
                      <div className="text-xs text-slate-500">{names.get(m.project_id)} · {shortDate(m.due_date, data.today)} · {m.open} of {m.total} open</div>
                    </li>
                  );
                })}
              </ul>
            )}
          </Card>
          <Card title="Capacity concerns" action={<Link className="link text-sm" href="/workload">Workload</Link>}>
            {data.workload_concerns.length === 0 ? <p className="text-sm text-slate-500">None found.</p> : (
              <ul className="space-y-2 text-sm">
                {data.workload_concerns.map((w, i) => (
                  <li key={i}><span className="font-medium">{w.person_name}</span> <span className="text-slate-600">{w.reason}</span></li>
                ))}
              </ul>
            )}
          </Card>
        </div>
      </div>

      <Card>
        <div className="mb-3 flex flex-wrap items-end gap-3">
          <div className="min-w-64 flex-1">
            <h2 className="text-[15px] font-semibold text-slate-900">Projects</h2>
            <p className="text-xs text-slate-500">Subprojects sit under their parent. A parent row counts its subprojects too, as Redmine does.</p>
          </div>
          <input aria-label="Search projects" placeholder="Search projects" value={q} onChange={(e) => setQ(e.target.value)} className="field w-52" />
          <select aria-label="Health filter" value={filter} onChange={(e) => setFilter(e.target.value as HealthState | "")} className="field">
            <option value="">All health</option>
            {ORDER.map((s) => <option key={s} value={s}>{HEALTH_LABEL[s]}</option>)}
          </select>
        </div>
        <div className="-mx-5 overflow-x-auto px-5">
          <table className="w-full min-w-[960px] text-left text-sm">
            <thead className="border-b border-slate-200 text-xs text-slate-500">
              <tr>
                {["Project", "Health", "Open work · dev / awaiting QA", "Overdue", "Blocked", "Estimated", "Next milestone", "Forecast vs baseline"].map((h) =>
                  <th key={h} className="whitespace-nowrap px-2 py-2.5 font-medium">{h}</th>)}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {rows.map(({ p, depth, children }) => <ProjectRow key={p.id} p={p} depth={depth} kids={children}
                open={!collapsed.has(p.id)} onToggle={() => toggle(p.id)} today={data.today} />)}
            </tbody>
          </table>
        </div>
        {rows.length === 0 && <Empty>{ps.length === 0 ? "No projects are visible to your account." : "No projects match."}</Empty>}
      </Card>
    </>
  );
}

function ProjectRow({ p, depth, kids, open, onToggle, today }: { p: ProjectSummary; depth: number; kids: number; open: boolean; onToggle: () => void; today: string }) {
  // A parent shows totals over itself and its visible subprojects.
  const c = p.rollup ?? p.issues;
  const dev = c.open - c.awaiting_qa;
  const coverage = p.rollup ? (p.rollup_estimate_coverage ?? null) : p.estimate_coverage;
  const remaining = p.rollup ? (p.rollup_remaining_estimated_hours ?? null) : p.remaining_estimated_hours;
  const max = Math.max(c.open, 1);
  return (
    <tr className={`hover:bg-slate-50 ${depth > 0 ? "bg-slate-50/60" : ""}`}>
      <td className="px-2 py-3">
        <div className="flex items-center gap-1.5" style={{ paddingLeft: depth * 24 }}>
          {kids > 0 ? (
            <button onClick={onToggle} aria-expanded={open} aria-label={`${open ? "Hide" : "Show"} subprojects`}
              className="grid h-6 w-6 place-items-center rounded text-slate-500 hover:bg-slate-200">
              <svg viewBox="0 0 20 20" className={`h-4 w-4 transition ${open ? "rotate-90" : ""}`} fill="currentColor" aria-hidden><path d="M7 5l6 5-6 5z" /></svg>
            </button>
          ) : <span className="w-6" aria-hidden>{depth > 0 && <span className="ml-2 inline-block h-3 w-2.5 border-b border-l border-slate-300" />}</span>}
          <div className="min-w-0">
            <Link className={`${depth > 0 ? "font-medium" : "font-semibold"} text-blue-700 hover:text-blue-900 hover:underline`} href={`/projects/${p.id}`}>{p.name}</Link>
            {p.rollup && <div className="text-xs text-slate-500">incl. {(p.subproject_ids ?? []).length} subproject{(p.subproject_ids ?? []).length === 1 ? "" : "s"}</div>}
          </div>
        </div>
      </td>
      <td className="px-2 py-3" title={p.health.reasons.join("; ")}><HealthBadge state={p.health.state} /></td>
      <td className="px-2 py-3">
        <div className="flex items-center gap-2" title={`${dev} in development, ${c.awaiting_qa} awaiting QA`}>
          <div className="flex h-2 w-24 overflow-hidden rounded-full bg-slate-200" aria-hidden>
            <div className="bg-blue-700" style={{ width: `${(dev / max) * 100}%` }} />
            <div className="bg-blue-300" style={{ width: `${(c.awaiting_qa / max) * 100}%` }} />
          </div>
          <span className="whitespace-nowrap tabular-nums">{dev}{c.awaiting_qa > 0 && <span className="text-slate-500"> · {c.awaiting_qa} QA</span>}</span>
        </div>
      </td>
      <td className="px-2 py-3 font-semibold tabular-nums text-amber-700">{c.overdue || ""}</td>
      <td className="px-2 py-3 font-semibold tabular-nums text-red-700">{c.blocked || ""}</td>
      <td className="px-2 py-3" title={`Remaining estimated: ${hours(remaining)}`}>
        <div className="flex items-center gap-2"><Progress value={coverage} className="w-12" label="Estimate coverage" /><span className="tabular-nums">{pct(coverage)}</span></div>
      </td>
      <td className="px-2 py-3">
        {p.next_milestone ? (
          <div><div>{p.next_milestone.name}</div><div className="text-xs text-slate-500">{shortDate(p.next_milestone.due_date, today)} · {relDays(p.next_milestone.due_date, today)}</div></div>
        ) : <span className="text-slate-500">TBD</span>}
      </td>
      <td className="px-2 py-3">
        <div className="flex items-center gap-2 whitespace-nowrap">
          <span>{shortDate(p.delivery?.forecast, today)}</span>
          <DeliveryDelta d={p.delivery} />
        </div>
      </td>
    </tr>
  );
}
