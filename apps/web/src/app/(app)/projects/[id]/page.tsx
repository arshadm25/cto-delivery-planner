"use client";

import { use, useState } from "react";
import { api, Flag, FLAG_LABEL, Issue, Milestone, ProjectSummary } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { ago, date, hours, pct, tbd } from "@/lib/format";
import { can, useMe } from "@/components/Shell";
import { Card, ErrorBox, FlagChip, HealthBadge, Loading, Pager, SeverityBadge, Stat, Table } from "@/components/ui";
import { DeliveryPanel } from "@/components/DeliveryPanel";
import { IssuesCard } from "@/components/IssueEditor";
import { MilestonesCard } from "@/components/MilestoneEditor";

interface ProjectResponse {
  project: ProjectSummary;
  blockers: Flag[];
  data_gaps: string[];
}

export default function ProjectPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data, error, loading, reload } = useApi<ProjectResponse>(`/projects/${id}`);
  const ms = useApi<{ milestones: Milestone[] }>(`/projects/${id}/milestones`);
  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;
  const p = data.project;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{p.name}</h1>
        <HealthBadge state={p.health.state} />
        {p.redmine_url && <a href={p.redmine_url} target="_blank" rel="noreferrer" className="text-sm text-sky-700 hover:underline">Open in Redmine</a>}
        <span className="text-sm text-slate-500">Last activity {ago(p.last_activity)}</span>
      </div>
      {p.health.reasons.length > 0 && <p className="text-sm text-slate-600">Why: {p.health.reasons.join(", ")}</p>}

      <RolesCard project={p} onSaved={reload} />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-7">
        <Stat label="Open" value={p.issues.open} />
        <Stat label="Awaiting QA" value={p.issues.awaiting_qa} />
        <Stat label="Closed" value={p.issues.closed} />
        <Stat label="Overdue" value={p.issues.overdue} tone={p.issues.overdue ? "warn" : undefined} />
        <Stat label="Blocked" value={p.issues.blocked} tone={p.issues.blocked ? "bad" : undefined} />
        <Stat label="Remaining estimate" value={hours(p.remaining_estimated_hours)} />
        <Stat label="Open dev issues estimated" value={pct(p.estimate_coverage)} />
      </div>

      <DeliveryPanel project={p} milestones={ms.data?.milestones ?? []} onChange={() => { reload(); ms.reload(); }} />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Blockers and risks">
          {data.blockers.length === 0 ? <p className="text-sm text-slate-500">No critical or high flags.</p> : (
            <ul className="space-y-3 text-sm">
              {data.blockers.map((f, i) => (
                <li key={i}>
                  <div className="flex items-center gap-2"><SeverityBadge severity={f.severity} /> <span className="font-medium">{f.title}</span></div>
                  <div className="text-slate-600">{f.evidence}</div>
                  <div className="text-slate-500">Next: {f.action}</div>
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card title="Missing data that limits forecast confidence">
          {data.data_gaps.length === 0 ? <p className="text-sm text-slate-500">None found.</p> : (
            <ul className="list-disc space-y-1 pl-5 text-sm">{data.data_gaps.map((g) => <li key={g}>{g}</li>)}</ul>
          )}
        </Card>
      </div>

      <MilestonesCard projectId={p.id} milestones={ms.data?.milestones ?? []} error={ms.error} onChange={() => { reload(); ms.reload(); }} />
      <IssuesCard projectId={p.id} milestones={ms.data?.milestones ?? []} onChange={() => { reload(); ms.reload(); }} />
    </div>
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
    <div className="flex flex-wrap items-center gap-4 text-sm">
      {editing ? (
        <>
          <label>Owner <input className="ml-1 rounded border px-2 py-1" value={owner} onChange={(e) => setOwner(e.target.value)} /></label>
          <label>Tech lead <input className="ml-1 rounded border px-2 py-1" value={lead} onChange={(e) => setLead(e.target.value)} /></label>
          <button onClick={save} className="rounded bg-slate-900 px-2 py-1 text-white">Save as verified</button>
          <button onClick={() => setEditing(false)} className="rounded border px-2 py-1">Cancel</button>
          <ErrorBox error={err} />
        </>
      ) : (
        <>
          <span>Owner: <b>{tbd(project.owner)}</b></span>
          <span>Tech lead: <b>{tbd(project.tech_lead)}</b></span>
          {project.roles_verified_at && <span className="text-slate-500">verified {date(project.roles_verified_at)}</span>}
          {can(me, "edit_project_roles") && <button onClick={() => setEditing(true)} className="rounded border px-2 py-1">Edit</button>}
        </>
      )}
    </div>
  );
}
