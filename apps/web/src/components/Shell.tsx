"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { createContext, useContext } from "react";
import { api, Me, ROLE_LABEL } from "@/lib/api";
import { useApi } from "@/lib/hooks";
import { ErrorBox, Loading } from "./ui";

const MeContext = createContext<Me | null>(null);

export function useMe(): Me {
  const me = useContext(MeContext);
  if (!me) throw new Error("useMe outside Shell");
  return me;
}

export function can(me: Me, permission: string) {
  return me.permissions.includes(permission);
}

const NAV = [
  { href: "/", label: "Overview" },
  { href: "/risks", label: "Risks" },
  { href: "/workload", label: "Workload" },
  { href: "/reports/weekly", label: "Weekly report" },
  { href: "/sync", label: "Sync status" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const { data: me, error, loading } = useApi<Me>("/auth/me");
  const path = usePathname();

  if (loading && !me) return <Loading />;
  if (error || !me) return <div className="p-6"><ErrorBox error={error} /></div>;

  const nav = [...NAV];
  if (me.permissions.includes("edit_capacity")) nav.splice(3, 0, { href: "/capacity", label: "Capacity" });
  if (me.permissions.includes("edit_settings")) nav.push({ href: "/settings", label: "Settings" });
  if (me.permissions.includes("manage_users")) nav.push({ href: "/admin/users", label: "Users" });

  async function logout() {
    await api("/auth/logout", { method: "POST" });
    window.location.href = "/login";
  }

  return (
    <MeContext.Provider value={me}>
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <Link href="/" className="font-semibold text-slate-900">CTO Delivery Planner</Link>
          <nav className="flex flex-wrap gap-1 text-sm">
            {nav.map((n) => {
              const active = n.href === "/" ? path === "/" : path.startsWith(n.href);
              return (
                <Link key={n.href} href={n.href}
                  className={`rounded px-2.5 py-1.5 ${active ? "bg-slate-900 text-white" : "text-slate-600 hover:bg-slate-100"}`}>
                  {n.label}
                </Link>
              );
            })}
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm text-slate-600">
            <span>{me.user.display_name} · {ROLE_LABEL[me.user.role]}</span>
            <button onClick={logout} className="rounded border border-slate-300 px-2 py-1 hover:bg-slate-100">Log out</button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-7xl px-4 py-6">{children}</main>
    </MeContext.Provider>
  );
}
