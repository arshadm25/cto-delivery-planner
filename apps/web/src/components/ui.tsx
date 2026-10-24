"use client";

import Link from "next/link";
import { DeliveryDates, FLAG_LABEL, HEALTH_LABEL, HealthState, Severity } from "@/lib/api";
import { daysBetween } from "@/lib/format";

export function Loading() {
  return (
    <div className="space-y-4 p-2" aria-busy="true">
      <span className="sr-only">Loading…</span>
      <div className="h-7 w-64 animate-pulse rounded-lg bg-slate-200" />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {[0, 1, 2, 3].map((i) => <div key={i} className="h-24 animate-pulse rounded-xl bg-slate-200" />)}
      </div>
      <div className="h-64 animate-pulse rounded-xl bg-slate-200" />
    </div>
  );
}

export function ErrorBox({ error }: { error: Error | null }) {
  if (!error) return null;
  return <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">{error.message}</div>;
}

export function PageHeader({ title, subtitle, children, crumbs }: {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  children?: React.ReactNode;
  crumbs?: { href: string; label: string }[];
}) {
  return (
    <div className="space-y-1">
      {crumbs && crumbs.length > 0 && (
        <nav aria-label="Breadcrumb" className="text-sm text-slate-500">
          {crumbs.map((c) => (
            <span key={c.href}><Link className="link" href={c.href}>{c.label}</Link><span className="mx-1.5" aria-hidden>›</span></span>
          ))}
        </nav>
      )}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h1 className="text-2xl font-bold tracking-tight text-slate-900">{title}</h1>
        {children}
      </div>
      {subtitle && <p className="text-sm text-slate-500">{subtitle}</p>}
    </div>
  );
}

export const HEALTH_COLOR: Record<HealthState, { badge: string; bar: string; dot: string }> = {
  blocked: { badge: "bg-red-100 text-red-800 ring-red-200", bar: "bg-red-700", dot: "bg-red-600" },
  at_risk: { badge: "bg-amber-100 text-amber-900 ring-amber-200", bar: "bg-amber-500", dot: "bg-amber-500" },
  insufficient_data: { badge: "bg-slate-200 text-slate-700 ring-slate-300", bar: "bg-slate-400", dot: "bg-slate-400" },
  on_track: { badge: "bg-emerald-100 text-emerald-800 ring-emerald-200", bar: "bg-emerald-600", dot: "bg-emerald-600" },
};

export function HealthBadge({ state }: { state: HealthState }) {
  return (
    <span className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-0.5 text-xs font-semibold ring-1 ring-inset ${HEALTH_COLOR[state].badge}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${HEALTH_COLOR[state].dot}`} aria-hidden />
      {HEALTH_LABEL[state]}
    </span>
  );
}

const SEV_STYLE: Record<Severity, string> = {
  critical: "bg-red-700 text-white",
  high: "bg-amber-100 text-amber-900",
  medium: "bg-blue-100 text-blue-900",
  low: "bg-slate-100 text-slate-700",
};

export function SeverityBadge({ severity }: { severity: Severity }) {
  return <span className={`inline-block whitespace-nowrap rounded-md px-1.5 py-0.5 text-[11px] font-bold uppercase tracking-wide ${SEV_STYLE[severity]}`}>{severity}</span>;
}

export function FlagChip({ kind }: { kind: string }) {
  return <span className="inline-block whitespace-nowrap rounded-md bg-slate-100 px-1.5 py-0.5 text-xs text-slate-700 ring-1 ring-inset ring-slate-200">{FLAG_LABEL[kind] ?? kind}</span>;
}

type Tone = "bad" | "warn" | "ok" | "neutral";
const TONE_CHIP: Record<Tone, string> = {
  bad: "bg-red-100 text-red-800",
  warn: "bg-amber-100 text-amber-900",
  ok: "bg-emerald-100 text-emerald-800",
  neutral: "bg-slate-100 text-slate-700",
};

export function Badge({ tone = "neutral", children, title }: { tone?: Tone; children: React.ReactNode; title?: string }) {
  return <span title={title} className={`inline-block whitespace-nowrap rounded-md px-2 py-0.5 text-xs font-semibold ${TONE_CHIP[tone]}`}>{children}</span>;
}

export function Card({ title, action, children, className = "" }: { title?: React.ReactNode; action?: React.ReactNode; children: React.ReactNode; className?: string }) {
  return (
    <section className={`rounded-xl border border-slate-200 bg-white p-5 shadow-xs ${className}`}>
      {(title || action) && (
        <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
          {title && <h2 className="text-[15px] font-semibold text-slate-900">{title}</h2>}
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

const TONE_TEXT: Record<Tone, string> = { bad: "text-red-700", warn: "text-amber-700", ok: "text-emerald-700", neutral: "text-slate-900" };

export function Stat({ label, value, tone, sub }: { label: string; value: React.ReactNode; tone?: Tone; sub?: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-xs">
      <div className="text-xs font-medium text-slate-500">{label}</div>
      <div className={`mt-1 text-2xl font-bold tabular-nums ${TONE_TEXT[tone ?? "neutral"]}`}>{value}</div>
      {sub && <div className="mt-0.5 text-xs text-slate-500">{sub}</div>}
    </div>
  );
}

export function Progress({ value, className = "w-16", color = "bg-slate-500", label }: { value: number | null | undefined; className?: string; color?: string; label?: string }) {
  const v = Math.max(0, Math.min(1, value ?? 0));
  return (
    <div className={`h-1.5 overflow-hidden rounded-full bg-slate-200 ${className}`} role="progressbar" aria-label={label}
      aria-valuemin={0} aria-valuemax={100} aria-valuenow={value == null ? undefined : Math.round(v * 100)}>
      <div className={`h-full rounded-full ${color}`} style={{ width: `${v * 100}%` }} />
    </div>
  );
}

export function Table({ head, children }: { head: string[]; children: React.ReactNode }) {
  return (
    <div className="-mx-5 overflow-x-auto px-5">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-slate-200 text-xs text-slate-500">
          <tr>{head.map((h, i) => <th key={h + i} className="whitespace-nowrap px-2 py-2.5 font-medium">{h}</th>)}</tr>
        </thead>
        <tbody className="divide-y divide-slate-100 [&>tr:hover]:bg-slate-50">{children}</tbody>
      </table>
    </div>
  );
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <p className="py-6 text-center text-sm text-slate-500">{children}</p>;
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: string; count?: number }[]; value: T; onChange: (t: T) => void }) {
  return (
    <div role="tablist" className="flex gap-1 overflow-x-auto border-b border-slate-200">
      {tabs.map((t) => (
        <button key={t.id} role="tab" aria-selected={value === t.id} onClick={() => onChange(t.id)}
          className={`-mb-px whitespace-nowrap border-b-2 px-3.5 py-2.5 text-sm transition ${value === t.id ? "border-blue-700 font-semibold text-slate-900" : "border-transparent text-slate-500 hover:text-slate-800"}`}>
          {t.label}
          {t.count !== undefined && t.count > 0 && <span className="ml-1.5 rounded-full bg-slate-200 px-1.5 text-xs text-slate-700">{t.count}</span>}
        </button>
      ))}
    </div>
  );
}

export function Pager({ page, perPage, total, onPage }: { page: number; perPage: number; total: number; onPage: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / perPage));
  return (
    <div className="mt-4 flex items-center gap-2 text-sm text-slate-600">
      <button disabled={page <= 1} onClick={() => onPage(page - 1)} className="btn btn-sm">Previous</button>
      <span>Page {page} of {pages} · {total} total</span>
      <button disabled={page >= pages} onClick={() => onPage(page + 1)} className="btn btn-sm">Next</button>
    </div>
  );
}

export function StaleBanner({ stale, lastSync }: { stale: boolean; lastSync: string | null }) {
  if (!stale) return null;
  return (
    <div role="status" className="rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900">
      {lastSync
        ? <>Data may be out of date: the last successful sync finished {new Date(lastSync).toLocaleString()}. See <Link className="underline" href="/sync">Sync status</Link>.</>
        : <>No successful sync yet. Figures below may be empty or incomplete. See <Link className="underline" href="/sync">Sync status</Link>.</>}
    </div>
  );
}

// Forecast against the approved baseline, as a short chip.
export function DeliveryDelta({ d }: { d?: DeliveryDates }) {
  if (!d) return <Badge>No forecast</Badge>;
  if (d.status === "delivered") return <Badge tone="ok">Delivered</Badge>;
  if (!d.forecast) return <Badge>No forecast</Badge>;
  if (!d.baseline) return <Badge>No baseline</Badge>;
  const n = daysBetween(d.baseline, d.forecast);
  if (n > 0) return <Badge tone="warn" title="Forecast is after the approved baseline">{n} d late</Badge>;
  return <Badge tone="ok" title="Forecast meets the approved baseline">{n === 0 ? "On time" : `${-n} d early`}</Badge>;
}

