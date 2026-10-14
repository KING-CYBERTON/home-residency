import { useLogout } from "@refinedev/core";
import { LogOut, Menu, X } from "lucide-react";
import { useState, type ReactNode } from "react";
import { NavLink, Outlet } from "react-router";

import { Button } from "@/components/ui/button";
import { canCall, roleLabels, useMe, type StaffRole } from "@/lib/access";
import { cn } from "@/lib/utils";
import { nav } from "@/resources";

function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  const me = useMe();
  const items = nav.filter((n) => canCall(me?.roles, n.op));
  const groups = [...new Set(items.map((i) => i.group))];
  return (
    <nav className="flex flex-col gap-4 p-3 text-sm">
      {groups.map((g) => (
        <div key={g || "top"}>
          {g && <div className="text-muted-foreground px-2 pb-1 text-xs font-medium tracking-wide uppercase">{g}</div>}
          {items
            .filter((i) => i.group === g)
            .map((i) => (
              <NavLink
                key={i.name}
                to={i.route}
                end={i.route === "/"}
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-md px-2 py-1.5 transition-colors",
                    isActive ? "bg-accent text-accent-foreground font-medium" : "text-foreground/80 hover:bg-accent/60",
                  )
                }
              >
                <i.icon className="size-4 shrink-0" />
                {i.label}
              </NavLink>
            ))}
        </div>
      ))}
    </nav>
  );
}

function Brand() {
  return (
    <div className="flex h-14 items-center gap-2 border-b px-4">
      <div className="bg-primary text-primary-foreground grid size-7 place-items-center rounded-md text-xs font-bold">HR</div>
      <div className="leading-tight">
        <div className="text-sm font-semibold">Homend Residence</div>
        <div className="text-muted-foreground text-xs">Management console</div>
      </div>
    </div>
  );
}

export function Layout({ children }: { children?: ReactNode }) {
  const me = useMe();
  const { mutate: logout } = useLogout();
  const [open, setOpen] = useState(false);
  return (
    <div className="flex min-h-svh">
      <aside className="bg-sidebar sticky top-0 hidden h-svh w-60 shrink-0 overflow-y-auto border-r lg:block">
        <Brand />
        <Sidebar />
      </aside>
      {open && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setOpen(false)} />
          <aside className="bg-sidebar absolute inset-y-0 left-0 w-64 overflow-y-auto border-r">
            <Brand />
            <Sidebar onNavigate={() => setOpen(false)} />
          </aside>
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="bg-background/95 sticky top-0 z-30 flex h-14 items-center gap-3 border-b px-4 backdrop-blur">
          <Button variant="ghost" size="icon" className="lg:hidden" onClick={() => setOpen((o) => !o)} aria-label="Menu">
            {open ? <X /> : <Menu />}
          </Button>
          <div className="flex-1" />
          {me && (
            <div className="min-w-0 text-right text-sm leading-tight">
              <div className="font-medium">{me.full_name}</div>
              <div
                className="text-muted-foreground hidden max-w-md truncate text-xs sm:block"
                title={(me.roles ?? []).map((r) => roleLabels[r as StaffRole] ?? r).join(", ")}
              >
                {(me.roles ?? []).map((r) => roleLabels[r as StaffRole] ?? r).join(", ")}
              </div>
            </div>
          )}
          <Button variant="ghost" size="icon" onClick={() => logout()} aria-label="Sign out" title="Sign out">
            <LogOut />
          </Button>
        </header>
        <main className="mx-auto w-full max-w-7xl flex-1 p-4 sm:p-6">{children ?? <Outlet />}</main>
      </div>
    </div>
  );
}

export function NoAccess() {
  return (
    <div className="text-muted-foreground py-20 text-center text-sm">
      Your roles do not include this area. Ask an admin if you need access.
    </div>
  );
}

/** Route guard: renders the page only for roles that may call its backing operation. */
export function Guard({ op, children }: { op: string; children: ReactNode }) {
  const me = useMe();
  if (!me) return null;
  return canCall(me.roles, op) ? <>{children}</> : <NoAccess />;
}
