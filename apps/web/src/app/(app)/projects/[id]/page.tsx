"use client";

import Link from "next/link";
import { use, useEffect, useState } from "react";
import { api, Flag, Milestone, Portfolio, ProjectSummary } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { ago, date, hours, pct, tbd } from "@/lib/format";
import { can, useMe } from "@/components/Shell";
import { Card, Empty, ErrorBox, HealthBadge, Loading, PageHeader, SeverityBadge, Stat, Tabs } from "@/components/ui";
import { Commitments, DeliverySummary, RiskHistory } from "@/components/DeliveryPanel";
import { IssuesCard } from "@/components/IssueEditor";
import { MilestonesCard } from "@/components/MilestoneEditor";

interface ProjectResponse {
  project: ProjectSummary;
  blockers: Flag[];
  data_gaps: string[];
}

type Tab = "risks" | "commitments" | "milestones" | "issues" | "history";
const TABS: Tab[] = ["risks", "commitments", "milestones", "issues", "history"];

export default function ProjectPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data, error, loading, reload } = useApi<ProjectResponse>(`/projects/${id}`);
  const ms = useApi<{ milestones: Milestone[] }>(`/projects/${id}/milestones`);
  // Names and health of the parent and subprojects come from the portfolio list.
  const portfolio = useApi<Portfolio>("/portfolio/summary");
  const [tab, setTab] = useState<Tab>("risks");
  useEffect(() => {
    const h = window.location.hash.slice(1) as Tab;
    if (TABS.includes(h)) setTab(h);
  }, []);
  const pick = (t: Tab) => { setTab(t); history.replaceState(null, "", `#${t}`); };

  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;
  const p = data.project;
  const c = p.rollup ?? p.issues;
  const today = portfolio.data?.today ?? new Date().toISOString().slice(0, 10);
  const byId = new Map((portfolio.data?.projects ?? []).map((x) => [x.id, x]));
  const parent = p.parent_id !== null ? byId.get(p.parent_id) : undefined;
  const subs = (p.subproject_ids ?? []).map((sid) => byId.get(sid)).filter((x): x is ProjectSummary => !!x);
  const dev = c.open - c.awaiting_qa;
  const coverage = p.rollup ? (p.rollup_estimate_coverage ?? null) : p.estimate_coverage;
  const remaining = p.rollup ? (p.rollup_remaining_estimated_hours ?? null) : p.remaining_estimated_hours;
  const milestones = ms.data?.milestones ?? [];

  return (
    <>
      <PageHeader title={p.name}
        crumbs={[{ href: "/", label: "Overview" }, ...(parent ? [{ href: `/projects/${parent.id}`, label: parent.name }] : [])]}
        subtitle={<>{subs.length > 0 && <>Includes {subs.length} subproject{subs.length === 1 ? "" : "s"} · </>}Last activity {ago(p.last_activity)}</>}>
        <HealthBadge state={p.health.state} />
        <span className="flex-1" />
        {p.redmine_url && <a href={p.redmine_url} target="_blank" rel="noreferrer" className="btn">Open in Redmine ↗</a>}
      </PageHeader>

      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-slate-700">
        <RolesCard project={p} onSaved={reload} />
        {p.health.reasons.length > 0 && <span className="text-slate-500">Why {p.health.state.replaceAll("_", " ")}: {p.health.reasons.join(", ")}</span>}
      </div>

      <DeliverySummary project={p} today={today} />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <Stat label="Open in dev" value={dev} sub={`+${c.awaiting_qa} awaiting QA`} />
        <Stat label="Overdue" value={c.overdue} tone={c.overdue ? "warn" : undefined} sub="past their due date" />
        <Stat label="Blocked" value={c.blocked} tone={c.blocked ? "bad" : undefined} sub="waiting on other work" />
        <Stat label="Remaining estimate" value={hours(remaining)} sub={c.open_no_estimate ? `+${c.open_no_estimate} unestimated` : "all estimated"} />
        <Stat label="Estimated" value={pct(coverage)} sub="of open dev issues" />
        <Stat label="Closed" value={c.closed} sub={`of ${c.total} issues`} />
      </div>

      {subs.length > 0 && (
        <Card title="Subprojects" action={<span className="text-xs text-slate-500">The numbers above include these</span>}>
          <div className="grid gap-3 md:grid-cols-2">
            {subs.map((s) => (
              <Link key={s.id} href={`/projects/${s.id}`} className="flex items-center gap-3 rounded-lg border border-slate-200 px-4 py-3 transition hover:border-blue-300 hover:bg-blue-50/40">
                <HealthBadge state={s.health.state} />
                <span className="flex-1 font-semibold text-slate-900">{s.name}</span>
                <span className="text-sm text-slate-500">{s.issues.open - s.issues.awaiting_qa} open · {s.issues.awaiting_qa} QA{s.issues.overdue ? ` · ${s.issues.overdue} overdue` : ""}</span>
                <span className="text-blue-700" aria-hidden>›</span>
              </Link>
            ))}
          </div>
        </Card>
      )}

      <div className="space-y-4">
        <Tabs value={tab} onChange={pick} tabs={[
          { id: "risks", label: "Risks and gaps", count: data.blockers.length },
          { id: "commitments", label: "Commitments" },
          { id: "milestones", label: "Milestones", count: milestones.length },
          { id: "issues", label: "Issues", count: c.open },
          { id: "history", label: "History" },
        ]} />

        {tab === "risks" && (
          <div className="grid gap-4 lg:grid-cols-3">
            <RisksCard projectId={p.id} />
            <Card title="Missing data that limits forecast confidence">
              {data.data_gaps.length === 0 ? <p className="text-sm text-slate-500">None found.</p> : (
                <ul className="space-y-2 text-sm">{data.data_gaps.map((g) => <li key={g} className="flex gap-2"><span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-slate-400" aria-hidden />{g}</li>)}</ul>
              )}
            </Card>
          </div>
        )}
        {tab === "commitments" && <Commitments project={p} milestones={milestones} onChange={() => { reload(); ms.reload(); }} />}
        {tab === "milestones" && <MilestonesCard projectId={p.id} milestones={milestones} error={ms.error} onChange={() => { reload(); ms.reload(); }} />}
        {tab === "issues" && <IssuesCard projectId={p.id} milestones={milestones} onChange={() => { reload(); ms.reload(); }} />}
        {tab === "history" && <RiskHistory projectId={p.id} />}
      </div>
    </>
  );
}

function RisksCard({ projectId }: { projectId: number }) {
  const [all, setAll] = useState(false);
  const { data, error } = useApi<{ flags: Flag[]; total: number }>(`/risks?project=${projectId}&per_page=200`);
  const flags = data?.flags ?? [];
  const serious = flags.filter((f) => f.severity === "critical" || f.severity === "high");
  const shown = all || serious.length === 0 ? flags : serious;
  return (
    <Card title="Blockers and risks" className="lg:col-span-2"
      action={flags.length > serious.length && serious.length > 0 && (
        <button className="link text-sm" onClick={() => setAll(!all)}>{all ? "Show critical and high only" : `Show all ${flags.length}, including medium and low`}</button>
      )}>
      <ErrorBox error={error} />
      {data && flags.length === 0 && <Empty>No flags on this project.</Empty>}
      <ul className="-my-1 divide-y divide-slate-100">
        {shown.map((f, i) => (
          <li key={i} className="flex gap-3 py-3 text-sm">
            <div className="w-16 shrink-0 pt-0.5"><SeverityBadge severity={f.severity} /></div>
            <div className="min-w-0 space-y-0.5">
              {f.redmine_url ? <a className="link" href={f.redmine_url} target="_blank" rel="noreferrer">{f.title}</a> : <div className="font-medium">{f.title}</div>}
              <div className="text-slate-500">{f.evidence}</div>
              <div className="text-slate-700">Next: {f.action}</div>
            </div>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function RolesCard({ project, onSaved }: { project: ProjectSummary; onSaved: () => void }) {
  const me = useMe();
  const [editing, setEditing] = useState(false);
  const [owner, setOwner] = useState(project.owner ?? "");
  const [lead, setLead] = useState(project.tech_lead ?? "");
  const [err, setErr] = useState<Error | null>(null);

  async function save() {
    try {
      await api(`/projects/${project.id}/roles`, { method: "PUT", body: { owner, tech_lead: lead } });
      setEditing(false);
      onSaved();
    } catch (e) {
      setErr(e as Error);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-3">
      {editing ? (
        <>
          <label className="flex items-center gap-1.5">Owner <input className="field" value={owner} onChange={(e) => setOwner(e.target.value)} /></label>
          <label className="flex items-center gap-1.5">Tech lead <input className="field" value={lead} onChange={(e) => setLead(e.target.value)} /></label>
          <button onClick={save} className="btn btn-primary btn-sm">Save as verified</button>
          <button onClick={() => setEditing(false)} className="btn btn-sm">Cancel</button>
          <ErrorBox error={err} />
        </>
      ) : (
        <>
          <span>Owner: <b>{tbd(project.owner)}</b></span>
          <span>Tech lead: <b>{tbd(project.tech_lead)}</b></span>
          {project.roles_verified_at && <span className="text-slate-500">verified {date(project.roles_verified_at)}</span>}
          {can(me, "edit_project_roles") && <button onClick={() => setEditing(true)} className="btn btn-sm">Edit</button>}
        </>
      )}
    </div>
  );
}
