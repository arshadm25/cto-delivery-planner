"use client";

import { SyncStatusResponse } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { ago, dateTime } from "@/lib/format";
import { Card, ErrorBox, Loading, Stat, Table } from "@/components/ui";

const OUTCOME_STYLE: Record<string, string> = {
  succeeded: "text-emerald-700",
  partial: "text-amber-700",
  failed: "text-red-700",
  abandoned: "text-red-700",
  running: "text-blue-700",
};

export default function SyncPage() {
  const { data, error, loading } = useApi<SyncStatusResponse>("/sync/status");
  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;
  const st = data.status;
  const latest = st.runs[0];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight text-slate-900">Sync status</h1>
      {data.latest_failed && (
        <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
          The latest sync run ({latest.kind}, {dateTime(latest.started_at)}) {latest.outcome === "partial" ? "only partly succeeded" : latest.outcome}.
          {latest.error_summary && <> Error: {latest.error_summary}.</>}
          {data.showing_previous && <> The dashboard keeps showing data from the last successful run for anything that failed.</>}
        </div>
      )}
      {data.data_stale && !data.latest_failed && (
        <div role="status" className="rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900">
          No successful sync in the last {data.stale_after}.
        </div>
      )}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Last successful sync" value={ago(st.last_success?.finished_at)} tone={data.data_stale ? "warn" : "ok"} />
        <Stat label="Latest run" value={latest ? latest.outcome : "none"} tone={data.latest_failed ? "bad" : undefined} />
        <Stat label="Checkpoint (Redmine time)" value={<span className="text-base">{dateTime(st.checkpoint)}</span>} />
        <Stat label="Projects with total mismatch" value={st.latest_reconciliation.filter((r) => !r.matches).length}
          tone={st.latest_reconciliation.some((r) => !r.matches) ? "warn" : "ok"} />
      </div>

      <Card title="Recent runs">
        <Table head={["Run", "Kind", "Started", "Finished", "Outcome", "Issues fetched / created / updated / inactivated", "Notes"]}>
          {st.runs.map((r) => {
            const c = r.counts?.issues;
            return (
              <tr key={r.id}>
                <td className="px-2 py-2">{r.id}</td>
                <td className="px-2 py-2">{r.kind}</td>
                <td className="px-2 py-2">{dateTime(r.started_at)}</td>
                <td className="px-2 py-2">{r.finished_at ? dateTime(r.finished_at) : "…"}</td>
                <td className={`px-2 py-2 font-medium ${OUTCOME_STYLE[r.outcome] ?? ""}`}>{r.outcome}</td>
                <td className="px-2 py-2">{c ? `${c.fetched} / ${c.created} / ${c.updated} / ${c.inactivated}` : "–"}</td>
                <td className="px-2 py-2 text-slate-600">
                  {r.warnings.length > 0 && <details><summary>{r.warnings.length} note(s)</summary><ul className="list-disc pl-5">{r.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul></details>}
                </td>
              </tr>
            );
          })}
        </Table>
        {st.runs.length === 0 && <p className="text-sm text-slate-500">No sync has run yet.</p>}
      </Card>

      <Card title="Reconciliation with Redmine totals (latest)">
        <Table head={["Project ID", "Redmine total", "Stored active", "Match", "Note"]}>
          {st.latest_reconciliation.map((r) => (
            <tr key={r.project_id}>
              <td className="px-2 py-2">{r.project_id}</td>
              <td className="px-2 py-2">{r.api_total ?? "unknown"}</td>
              <td className="px-2 py-2">{r.stored_active}</td>
              <td className={`px-2 py-2 ${r.matches ? "text-emerald-700" : "text-amber-700"}`}>{r.matches ? "yes" : "no"}</td>
              <td className="px-2 py-2 text-slate-600">{r.note}</td>
            </tr>
          ))}
        </Table>
      </Card>
    </div>
  );
}
