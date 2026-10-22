"use client";

import { FLAG_LABEL, HEALTH_LABEL, HealthState, Severity } from "@/lib/api";

export function Loading() {
  return <p className="p-6 text-sm text-slate-500">Loading…</p>;
}

export function ErrorBox({ error }: { error: Error | null }) {
  if (!error) return null;
  return <div role="alert" className="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-800">{error.message}</div>;
}

const HEALTH_STYLE: Record<HealthState, string> = {
  on_track: "bg-emerald-100 text-emerald-800",
  at_risk: "bg-amber-100 text-amber-900",
  blocked: "bg-red-100 text-red-800",
  insufficient_data: "bg-slate-200 text-slate-700",
};

export function HealthBadge({ state }: { state: HealthState }) {
  return <span className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${HEALTH_STYLE[state]}`}>{HEALTH_LABEL[state]}</span>;
}

const SEV_STYLE: Record<Severity, string> = {
  critical: "bg-red-600 text-white",
  high: "bg-amber-500 text-white",
  medium: "bg-sky-100 text-sky-900",
  low: "bg-slate-100 text-slate-700",
};

export function SeverityBadge({ severity }: { severity: Severity }) {
  return <span className={`inline-block rounded px-1.5 py-0.5 text-xs font-medium ${SEV_STYLE[severity]}`}>{severity}</span>;
}

export function FlagChip({ kind }: { kind: string }) {
  return <span className="inline-block rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-700">{FLAG_LABEL[kind] ?? kind}</span>;
}

export function Card({ title, children, className = "" }: { title?: string; children: React.ReactNode; className?: string }) {
  return (
    <section className={`rounded-lg border border-slate-200 bg-white p-4 ${className}`}>
      {title && <h2 className="mb-3 text-sm font-semibold text-slate-700">{title}</h2>}
      {children}
    </section>
  );
}

export function Stat({ label, value, tone }: { label: string; value: React.ReactNode; tone?: "bad" | "warn" | "ok" }) {
  const color = tone === "bad" ? "text-red-700" : tone === "warn" ? "text-amber-700" : tone === "ok" ? "text-emerald-700" : "text-slate-900";
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className={`mt-1 text-2xl font-semibold ${color}`}>{value}</div>
    </div>
  );
}

export function Table({ head, children }: { head: string[]; children: React.ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-slate-200 text-xs uppercase tracking-wide text-slate-500">
          <tr>{head.map((h) => <th key={h} className="px-2 py-2 font-medium">{h}</th>)}</tr>
        </thead>
        <tbody className="divide-y divide-slate-100">{children}</tbody>
      </table>
    </div>
  );
}

export function Pager({ page, perPage, total, onPage }: { page: number; perPage: number; total: number; onPage: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / perPage));
  return (
    <div className="mt-3 flex items-center gap-2 text-sm text-slate-600">
      <button disabled={page <= 1} onClick={() => onPage(page - 1)} className="rounded border px-2 py-1 disabled:opacity-40">Previous</button>
      <span>Page {page} of {pages} · {total} total</span>
      <button disabled={page >= pages} onClick={() => onPage(page + 1)} className="rounded border px-2 py-1 disabled:opacity-40">Next</button>
    </div>
  );
}

export function StaleBanner({ stale, lastSync }: { stale: boolean; lastSync: string | null }) {
  if (!stale) return null;
  return (
    <div role="status" className="mb-4 rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
      {lastSync
        ? <>Data may be out of date: the last successful sync finished {new Date(lastSync).toLocaleString()}. See <a className="underline" href="/sync">Sync status</a>.</>
        : <>No successful sync yet. Figures below may be empty or incomplete. See <a className="underline" href="/sync">Sync status</a>.</>}
    </div>
  );
}
