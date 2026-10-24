"use client";

import Link from "next/link";
import { useState } from "react";
import { Flag, FLAG_LABEL } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { Card, ErrorBox, Pager, SeverityBadge, Table } from "@/components/ui";

export default function RisksPage() {
  const [kind, setKind] = useState("");
  const [severity, setSeverity] = useState("");
  const [page, setPage] = useState(1);
  const qs = new URLSearchParams({ page: String(page), per_page: "50" });
  if (kind) qs.set("kind", kind);
  if (severity) qs.set("severity", severity);
  const { data, error } = useApi<{ flags: Flag[]; total: number; page: number; per_page: number; counts_by_kind: Record<string, number> }>(`/risks?${qs}`);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold tracking-tight text-slate-900">Risks, blockers and data gaps</h1>
      <p className="text-sm text-slate-600">Each flag shows the evidence behind it and links to the source in Redmine. Thresholds are on the Settings page.</p>
      <div className="flex flex-wrap gap-2 text-sm">
        <select aria-label="Severity" value={severity} onChange={(e) => { setSeverity(e.target.value); setPage(1); }} className="field">
          <option value="">All severities</option>
          {["critical", "high", "medium", "low"].map((s) => <option key={s} value={s}>{s}</option>)}
        </select>
        <select aria-label="Kind" value={kind} onChange={(e) => { setKind(e.target.value); setPage(1); }} className="field">
          <option value="">All kinds</option>
          {Object.entries(FLAG_LABEL).map(([k, v]) => <option key={k} value={k}>{v}{data?.counts_by_kind[k] ? ` (${data.counts_by_kind[k]})` : ""}</option>)}
        </select>
      </div>
      <ErrorBox error={error} />
      <Card>
        <Table head={["Severity", "Project", "Finding", "Evidence", "Suggested next action"]}>
          {(data?.flags ?? []).map((f, i) => (
            <tr key={i}>
              <td className="px-2 py-2"><SeverityBadge severity={f.severity} /></td>
              <td className="px-2 py-2"><Link className="link" href={`/projects/${f.project_id}`}>{f.project_name}</Link></td>
              <td className="px-2 py-2">{f.redmine_url ? <a className="link" href={f.redmine_url} target="_blank" rel="noreferrer">{f.title}</a> : f.title}</td>
              <td className="px-2 py-2 text-slate-600">{f.evidence}</td>
              <td className="px-2 py-2 text-slate-600">{f.action}</td>
            </tr>
          ))}
        </Table>
        {data && <Pager page={data.page} perPage={data.per_page} total={data.total} onPage={setPage} />}
      </Card>
    </div>
  );
}
