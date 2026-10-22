"use client";

import Link from "next/link";
import { HEALTH_LABEL, HealthState, Portfolio } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { ago, date, hours, pct, tbd } from "@/lib/format";
import { Card, ErrorBox, HealthBadge, Loading, StaleBanner, Stat, Table } from "@/components/ui";

const ORDER: HealthState[] = ["blocked", "at_risk", "insufficient_data", "on_track"];

export default function Overview() {
  const { data, error, loading } = useApi<Portfolio>("/portfolio/summary");
  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;

  const projects = [...data.projects].sort(
    (a, b) => ORDER.indexOf(a.health.state) - ORDER.indexOf(b.health.state) || a.name.localeCompare(b.name),
  );

  return (
    <div className="space-y-6">
      <StaleBanner stale={data.data_stale} lastSync={data.last_successful_sync} />
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">Portfolio overview</h1>
        <p className="text-sm text-slate-500">
          Last successful sync {ago(data.last_successful_sync)} · today {data.today}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-6">
        <Stat label="Projects" value={data.projects_in_scope} />
        {ORDER.map((s) => (
          <Stat key={s} label={HEALTH_LABEL[s]} value={data.by_health[s] ?? 0}
            tone={s === "blocked" ? "bad" : s === "at_risk" ? "warn" : s === "on_track" ? "ok" : undefined} />
        ))}
        <Stat label="Overdue issues" value={data.overdue_issues} tone={data.overdue_issues ? "warn" : undefined} />
        <Stat label="Blocked issues" value={data.blocked_issues} tone={data.critical_blocked ? "bad" : undefined} />
        <Stat label="Open issues" value={data.open_issues} />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card title="Projects" className="lg:col-span-2">
          <Table head={["Project", "Health", "Open", "Overdue", "Blocked", "Estimates", "Next milestone", "Forecast"]}>
            {projects.map((p) => (
              <tr key={p.id} className="hover:bg-slate-50">
                <td className="px-2 py-2"><Link className="font-medium text-slate-900 hover:underline" href={`/projects/${p.id}`}>{p.name}</Link></td>
                <td className="px-2 py-2" title={p.health.reasons.join("; ")}><HealthBadge state={p.health.state} /></td>
                <td className="px-2 py-2" title={`${p.issues.awaiting_qa} awaiting QA`}>{p.issues.open}{p.issues.awaiting_qa > 0 && <span className="text-xs text-slate-500"> ({p.issues.awaiting_qa} QA)</span>}</td>
                <td className="px-2 py-2">{p.issues.overdue || ""}</td>
                <td className="px-2 py-2">{p.issues.blocked || ""}</td>
                <td className="px-2 py-2" title={`Remaining estimated: ${hours(p.remaining_estimated_hours)}`}>{pct(p.estimate_coverage)}</td>
                <td className="px-2 py-2">{p.next_milestone ? `${p.next_milestone.name} · ${date(p.next_milestone.due_date)}` : "TBD"}</td>
                <td className="px-2 py-2">{p.delivery ? tbd(p.delivery.forecast) : "TBD"}</td>
              </tr>
            ))}
          </Table>
          {projects.length === 0 && <p className="text-sm text-slate-500">No projects are visible to your account.</p>}
        </Card>

        <div className="space-y-4">
          <Card title="Milestones in the next 30 days">
            {data.upcoming_milestones.length === 0 ? <p className="text-sm text-slate-500">None with a date.</p> : (
              <ul className="space-y-2 text-sm">
                {data.upcoming_milestones.map((m) => (
                  <li key={m.id}>
                    <Link href={`/projects/${m.project_id}`} className="font-medium hover:underline">{m.name}</Link>
                    <div className="text-slate-500">{date(m.due_date)} · {m.open} of {m.total} open</div>
                  </li>
                ))}
              </ul>
            )}
          </Card>
          {data.workload_concerns.length > 0 && (
            <Card title="Capacity concerns">
              <ul className="space-y-2 text-sm">
                {data.workload_concerns.map((w, i) => (
                  <li key={i}><span className="font-medium">{w.person_name}</span>: {w.reason}</li>
                ))}
              </ul>
            </Card>
          )}
        </div>
      </div>
    </div>
  );
}
