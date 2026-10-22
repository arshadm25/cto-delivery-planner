"use client";

import Link from "next/link";
import { API_BASE, Flag, MilestoneRef, WorkloadConcern } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { date } from "@/lib/format";
import { Card, ErrorBox, Loading, SeverityBadge } from "@/components/ui";

interface Change { project_id: number; project_name: string; health_before: string | null; health_now: string; forecast_before: string | null; forecast_now: string | null; baseline: string | null; causes: string[] }
interface Report {
  report_date: string;
  compared_with: string | null;
  changes: Change[];
  forecast_changes: Change[];
  milestones_due: MilestoneRef[];
  blockers: Flag[];
  capacity_conflicts: WorkloadConcern[];
  decisions_needed: string[];
  data_quality_asks: string[];
  notes: string[];
}

function List({ items, empty = "None." }: { items: React.ReactNode[]; empty?: string }) {
  if (items.length === 0) return <p className="text-sm text-slate-500">{empty}</p>;
  return <ul className="list-disc space-y-1 pl-5 text-sm">{items.map((x, i) => <li key={i}>{x}</li>)}</ul>;
}

export default function WeeklyReportPage() {
  const { data, error, loading } = useApi<Report>("/reports/weekly");
  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">Weekly delivery report · {data.report_date}</h1>
        <a className="rounded border px-3 py-1 text-sm hover:bg-slate-100" href={`${API_BASE}/reports/weekly?format=md`}>Download Markdown (names masked)</a>
      </div>
      {data.compared_with && <p className="text-sm text-slate-600">Compared with the snapshot of {data.compared_with}.</p>}
      <List items={data.notes} empty="" />
      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="What changed">
          <List items={data.changes.map((c) => <><Link className="font-medium hover:underline" href={`/projects/${c.project_id}`}>{c.project_name}</Link>: {c.health_before ?? "new"} → {c.health_now}{c.causes.length > 0 && <span className="text-slate-600"> ({c.causes.join("; ")})</span>}</>)} />
        </Card>
        <Card title="Forecast changes and causes">
          <List items={data.forecast_changes.map((c) => <><b>{c.project_name}</b>: {date(c.forecast_before)} → {date(c.forecast_now)} (baseline {date(c.baseline)}). <span className="text-slate-600">{c.causes.join("; ")}</span></>)} />
        </Card>
        <Card title="Milestones due in the next 14 days">
          <List items={data.milestones_due.map((m) => <>{m.name}: {date(m.due_date)}, {m.open} of {m.total} open</>)} />
        </Card>
        <Card title="Blockers and forecast risks">
          <List items={data.blockers.map((f) => <><SeverityBadge severity={f.severity} /> {f.project_name}: {f.title}. <span className="text-slate-600">{f.evidence}</span></>)} />
        </Card>
        <Card title="Capacity conflicts">
          <List items={data.capacity_conflicts.map((c) => <><b>{c.person_name}</b>: {c.reason}</>)} />
        </Card>
        <Card title="Decisions needed">
          <List items={data.decisions_needed} />
        </Card>
        <Card title="Data-quality asks for project managers" className="lg:col-span-2">
          <List items={data.data_quality_asks} />
        </Card>
      </div>
    </div>
  );
}
