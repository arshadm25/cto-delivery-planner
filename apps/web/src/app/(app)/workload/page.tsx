"use client";

import Link from "next/link";
import { useState } from "react";
import { useApi } from "@/lib/hooks";
import { hours } from "@/lib/format";
import { Card, ErrorBox, Loading, Table } from "@/components/ui";

interface Person {
  person_id: number;
  person_name: string;
  open_issues: number;
  remaining_estimated_hours: number | null;
  unestimated_open: number;
  due_in_period_hours: number | null;
  logged_hours_28d: number | null;
  available_hours: number | null;
  allocated_hours: number;
  capacity_status: "unknown" | "over" | "within";
  concern?: { reason: string };
}
interface Workload {
  period_start: string;
  period_end: string;
  people: Person[];
  unassigned: { project_id: number; project_name: string; open: number; unestimated: number }[] | null;
  time_entries_available: boolean;
  notes: string[];
}

export default function WorkloadPage() {
  const today = new Date().toISOString().slice(0, 10);
  const in4w = new Date(Date.now() + 27 * 86400000).toISOString().slice(0, 10);
  const [from, setFrom] = useState(today);
  const [to, setTo] = useState(in4w);
  const { data, error, loading } = useApi<Workload>(`/workload?from=${from}&to=${to}`);

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold tracking-tight text-slate-900">Team workload</h1>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <label>From <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="field" /></label>
        <label>To <input type="date" value={to} onChange={(e) => setTo(e.target.value)} className="field" /></label>
      </div>
      {loading && !data && <Loading />}
      <ErrorBox error={error} />
      {data && (
        <>
          <ul className="list-disc pl-5 text-sm text-slate-600">{data.notes.map((n) => <li key={n}>{n}</li>)}</ul>
          <Card>
            <Table head={["Person", "Open issues", "Unestimated", "Remaining estimate", "Due in period", "Available", "Allocated", "Logged (28 days)", "Capacity"]}>
              {data.people.map((p) => (
                <tr key={p.person_id}>
                  <td className="px-2 py-2">{p.person_name}</td>
                  <td className="px-2 py-2">{p.open_issues}</td>
                  <td className="px-2 py-2">{p.unestimated_open || ""}</td>
                  <td className="px-2 py-2">{hours(p.remaining_estimated_hours)}</td>
                  <td className="px-2 py-2">{hours(p.due_in_period_hours)}</td>
                  <td className="px-2 py-2">{p.available_hours === null ? "Not recorded" : hours(p.available_hours)}</td>
                  <td className="px-2 py-2">{p.allocated_hours ? hours(p.allocated_hours) : ""}</td>
                  <td className="px-2 py-2">{data.time_entries_available ? hours(p.logged_hours_28d) : "n/a"}</td>
                  <td className={`px-2 py-2 ${p.capacity_status === "over" ? "font-medium text-red-700" : "text-slate-600"}`} title={p.concern?.reason}>
                    {p.capacity_status === "over" ? `Over: ${p.concern?.reason}` : p.capacity_status === "within" ? "Within" : "Unknown"}
                  </td>
                </tr>
              ))}
            </Table>
          </Card>
          <Card title="Unassigned open work">
            <Table head={["Project", "Open unassigned", "of which unestimated"]}>
              {(data.unassigned ?? []).map((u) => (
                <tr key={u.project_id}>
                  <td className="px-2 py-2"><Link className="link" href={`/projects/${u.project_id}`}>{u.project_name}</Link></td>
                  <td className="px-2 py-2">{u.open}</td><td className="px-2 py-2">{u.unestimated}</td>
                </tr>
              ))}
            </Table>
          </Card>
        </>
      )}
    </div>
  );
}
