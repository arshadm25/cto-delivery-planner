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

// Outline icons, 24px grid, drawn with the current text colour.
const ICON: Record<string, string> = {
  overview: "M4 5h6v6H4zM14 5h6v4h-6zM14 13h6v6h-6zM4 15h6v4H4z",
  risks: "M12 4 2.5 20h19L12 4zM12 10v4M12 17h.01",
  workload: "M16 19v-1a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v1M9 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM22 19v-1a4 4 0 0 0-3-3.9M16 4.1a3 3 0 0 1 0 5.8",
  capacity: "M4 20V10M10 20V4M16 20v-7M22 20H2",
  report: "M7 3h7l5 5v13H7zM14 3v5h5M10 13h6M10 17h6",
  sync: "M20 11a8 8 0 0 0-14.9-3M4 4v4h4M4 13a8 8 0 0 0 14.9 3M20 20v-4h-4",
  settings: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-2.8 1.2V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-2.8-1.2l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.7 1.7 0 0 0 3 14H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.2-2.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.7 1.7 0 0 0 10 3V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 2.8 1.2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1A1.7 1.7 0 0 0 21 10h0a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z",
  users: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM19 8v6M22 11h-6",
};

function Icon({ name }: { name: string }) {
  return (
    <svg viewBox="0 0 24 24" className="h-[18px] w-[18px] shrink-0" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d={ICON[name]} />
    </svg>
  );
}

type NavItem = { href: string; label: string; icon: string };

const PRIMARY: NavItem[] = [
  { href: "/", label: "Overview", icon: "overview" },
  { href: "/risks", label: "Risks", icon: "risks" },
  { href: "/workload", label: "Workload", icon: "workload" },
  { href: "/reports/weekly", label: "Weekly report", icon: "report" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const { data: me, error, loading } = useApi<Me>("/auth/me");
  const path = usePathname();

  if (loading && !me) return <div className="mx-auto max-w-6xl p-6"><Loading /></div>;
  if (error || !me) return <div className="p-6"><ErrorBox error={error} /></div>;

  const primary = [...PRIMARY];
  // Everyone can see capacity and teams; only some roles can change them.
  primary.splice(3, 0, { href: "/capacity", label: "Capacity", icon: "capacity" });
  const admin: NavItem[] = [{ href: "/sync", label: "Sync status", icon: "sync" }];
  if (me.permissions.includes("edit_settings")) admin.push({ href: "/settings", label: "Settings", icon: "settings" });
  if (me.permissions.includes("manage_users")) admin.push({ href: "/admin/users", label: "Users", icon: "users" });

  async function logout() {
    await api("/auth/logout", { method: "POST" });
    window.location.href = "/login";
  }

  const item = (n: NavItem) => {
    const active = n.href === "/" ? path === "/" || path.startsWith("/projects") : path.startsWith(n.href);
    return (
      <Link key={n.href} href={n.href} aria-current={active ? "page" : undefined}
        className={`flex items-center gap-2.5 whitespace-nowrap rounded-lg px-3 py-2 text-sm transition ${active ? "bg-slate-700/80 font-medium text-white" : "text-slate-300 hover:bg-slate-800 hover:text-white"}`}>
        <Icon name={n.icon} />
        {n.label}
      </Link>
    );
  };

  return (
    <MeContext.Provider value={me}>
      <div className="flex min-h-screen flex-col md:flex-row">
        <aside className="bg-slate-900 text-slate-300 md:w-60 md:shrink-0">
          <div className="md:sticky md:top-0 md:flex md:h-screen md:flex-col">
          <div className="flex items-center gap-2.5 px-5 py-4 md:py-5">
            <div className="grid h-8 w-8 place-items-center rounded-lg bg-blue-600 text-sm font-bold text-white" aria-hidden>DP</div>
            <Link href="/" className="font-semibold text-white">Delivery Planner</Link>
          </div>
          <nav aria-label="Main" className="flex gap-1 overflow-x-auto px-3 pb-3 md:flex-1 md:flex-col md:overflow-visible">
            {primary.map(item)}
            <div className="mx-2 hidden h-px bg-slate-800 md:my-2 md:block" />
            {admin.map(item)}
          </nav>
          <div className="flex items-center gap-3 border-t border-slate-800 px-4 py-3">
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-slate-700 text-xs font-semibold text-white" aria-hidden>
              {me.user.display_name.split(/\s+/).map((w) => w[0]).slice(0, 2).join("").toUpperCase()}
            </div>
            <div className="min-w-0 flex-1 text-sm">
              <div className="truncate font-medium text-white">{me.user.display_name}</div>
              <div className="text-xs text-slate-400">{ROLE_LABEL[me.user.role]}</div>
            </div>
            <button onClick={logout} className="rounded-md border border-slate-700 px-2 py-1 text-xs text-slate-300 hover:bg-slate-800 hover:text-white">Log out</button>
          </div>
          </div>
        </aside>
        <main className="min-w-0 flex-1">
          <div className="mx-auto max-w-[1240px] space-y-6 px-4 py-6 md:px-8 md:py-8">{children}</div>
        </main>
      </div>
    </MeContext.Provider>
  );
}
