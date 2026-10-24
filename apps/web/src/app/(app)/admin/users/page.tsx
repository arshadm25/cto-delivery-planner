"use client";

import { useState } from "react";
import { api, Portfolio, Role, ROLE_LABEL, User } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { dateTime } from "@/lib/format";
import { Card, ErrorBox, Loading, Table } from "@/components/ui";

type Row = User & { project_ids: number[] };

export default function UsersPage() {
  const { data, error, loading, reload } = useApi<{ users: Row[] }>("/admin/users");
  const projects = useApi<Portfolio>("/portfolio/summary");
  const [err, setErr] = useState<Error | null>(null);
  if (loading && !data) return <Loading />;
  if (error || !data) return <ErrorBox error={error} />;

  async function act(fn: () => Promise<unknown>) {
    setErr(null);
    try { await fn(); reload(); } catch (e) { setErr(e as Error); }
  }

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold tracking-tight text-slate-900">Users</h1>
      <p className="text-sm text-slate-600">Administrators and the CTO see every project. Other roles see only the projects assigned here.</p>
      <ErrorBox error={err} />
      <NewUser onDone={reload} />
      <Card>
        <Table head={["Name", "Email", "Role", "Projects", "Last login", ""]}>
          {data.users.map((u) => (
            <tr key={u.id} className={u.disabled ? "opacity-50" : ""}>
              <td className="px-2 py-2">{u.display_name}</td>
              <td className="px-2 py-2">{u.email}</td>
              <td className="px-2 py-2">
                <select aria-label={`Role for ${u.email}`} value={u.role} onChange={(e) => act(() => api(`/admin/users/${u.id}`, { method: "PATCH", body: { role: e.target.value } }))} className="field">
                  {Object.entries(ROLE_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                </select>
              </td>
              <td className="px-2 py-2">
                {u.role === "admin" || u.role === "cto" ? "All" : (
                  <ProjectPicker ids={u.project_ids} options={projects.data?.projects.map((p) => ({ id: p.id, name: p.name })) ?? []}
                    onSave={(ids) => act(() => api(`/admin/users/${u.id}/projects`, { method: "PUT", body: { project_ids: ids } }))} />
                )}
              </td>
              <td className="px-2 py-2">{dateTime(u.last_login_at)}</td>
              <td className="px-2 py-2 space-x-2">
                <button className="btn btn-sm" onClick={() => act(() => api(`/admin/users/${u.id}`, { method: "PATCH", body: { disabled: !u.disabled } }))}>
                  {u.disabled ? "Enable" : "Disable"}
                </button>
                <button className="btn btn-sm" onClick={() => {
                  const pw = prompt("New password (at least 12 characters)");
                  if (pw) act(() => api(`/admin/users/${u.id}/password`, { method: "POST", body: { password: pw } }));
                }}>Reset password</button>
              </td>
            </tr>
          ))}
        </Table>
      </Card>
    </div>
  );
}

function ProjectPicker({ ids, options, onSave }: { ids: number[]; options: { id: number; name: string }[]; onSave: (ids: number[]) => void }) {
  const [open, setOpen] = useState(false);
  const [sel, setSel] = useState<number[]>(ids);
  if (!open) return <button className="link" onClick={() => setOpen(true)}>{ids.length} assigned</button>;
  return (
    <div className="max-h-56 space-y-1 overflow-y-auto rounded-lg border border-slate-200 p-2">
      {options.map((o) => (
        <label key={o.id} className="flex items-center gap-2">
          <input type="checkbox" checked={sel.includes(o.id)} onChange={(e) => setSel(e.target.checked ? [...sel, o.id] : sel.filter((x) => x !== o.id))} />
          {o.name}
        </label>
      ))}
      <button className="btn btn-primary btn-sm mt-1" onClick={() => { onSave(sel); setOpen(false); }}>Save</button>
    </div>
  );
}

function NewUser({ onDone }: { onDone: () => void }) {
  const [f, setF] = useState({ email: "", display_name: "", password: "", role: "viewer" as Role });
  const [err, setErr] = useState<Error | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    try {
      await api("/admin/users", { method: "POST", body: f });
      setF({ email: "", display_name: "", password: "", role: "viewer" });
      onDone();
    } catch (e) { setErr(e as Error); }
  }
  return (
    <Card title="Add user">
      <form onSubmit={submit} className="flex flex-wrap items-end gap-2 text-sm">
        <input required type="email" placeholder="Email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} className="field" />
        <input required placeholder="Display name" value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} className="field" />
        <input required type="password" minLength={12} placeholder="Initial password (12+)" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} className="field" />
        <select value={f.role} onChange={(e) => setF({ ...f, role: e.target.value as Role })} className="field">
          {Object.entries(ROLE_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <button className="btn btn-primary">Add</button>
        <ErrorBox error={err} />
      </form>
    </Card>
  );
}
