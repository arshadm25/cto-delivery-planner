export function tbd(v: string | number | null | undefined, suffix = ""): string {
  if (v === null || v === undefined || v === "") return "TBD";
  return `${v}${suffix}`;
}

export function hours(v: number | null | undefined): string {
  if (v === null || v === undefined) return "Unknown";
  return `${Math.round(v * 10) / 10} h`;
}

export function pct(v: number | null | undefined): string {
  if (v === null || v === undefined) return "n/a";
  return `${Math.round(v * 100)}%`;
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// Dates from the API are calendar dates ("2026-11-03") or midnight UTC
// timestamps. Only the date part is used, so no time zone can shift the day.
export function date(v: string | null | undefined): string {
  if (!v) return "TBD";
  const [y, m, d] = v.slice(0, 10).split("-").map(Number);
  if (!y || !m || !d) return v;
  return `${d} ${MONTHS[m - 1]} ${y}`;
}

// Like date() but without the year when it is the current year.
export function shortDate(v: string | null | undefined, today?: string): string {
  if (!v) return "TBD";
  const full = date(v);
  const year = (today ?? new Date().toISOString()).slice(0, 4);
  return full.endsWith(` ${year}`) ? full.slice(0, -5) : full;
}

// Whole days from a to b (both calendar dates); positive when b is later.
export function daysBetween(a: string, b: string): number {
  const ms = Date.parse(b.slice(0, 10) + "T00:00:00Z") - Date.parse(a.slice(0, 10) + "T00:00:00Z");
  return Math.round(ms / 86400000);
}

export function relDays(v: string | null | undefined, today: string): string {
  if (!v) return "";
  const n = daysBetween(today, v);
  if (n === 0) return "today";
  if (n === 1) return "tomorrow";
  if (n === -1) return "yesterday";
  return n > 0 ? `in ${n} days` : `${-n} days ago`;
}

export function dateTime(v: string | null | undefined): string {
  if (!v) return "never";
  const d = new Date(v);
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function ago(v: string | null | undefined): string {
  if (!v) return "never";
  const mins = Math.round((Date.now() - new Date(v).getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const h = Math.round(mins / 60);
  if (h < 48) return `${h} h ago`;
  return `${Math.round(h / 24)} days ago`;
}
