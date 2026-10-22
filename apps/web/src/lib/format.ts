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

export function date(v: string | null | undefined): string {
  if (!v) return "TBD";
  return v.slice(0, 10);
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
