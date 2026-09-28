"use client";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  Siren,
  LayoutDashboard,
  Server,
  ArrowLeftRight,
  FlaskConical,
  Settings,
  Search,
  Menu,
  Radio,
  ArrowUpRight,
  ShieldCheck,
  Command,
  ChevronRight,
  Rocket,
  LogOut,
  ListChecks,
  ScrollText,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { useEnvironment, useSession } from "./providers";
import { SessionScreen } from "./session-screen";
import { environmentSchema } from "@/lib/contracts";
import { cn } from "@/lib/utils";
import { workspaceHitPath, workspaceSearch } from "@/lib/search";
import { OperationsInbox } from "./operations-inbox";
const navigation = [
  { label: "Dashboard", href: "/", icon: LayoutDashboard },
  { label: "Services", href: "/services", icon: Server },
  { label: "Incidents", href: "/incidents", icon: Siren },
  { label: "RCA & Actions", href: "/rca", icon: ListChecks },
  { label: "Transactions", href: "/transactions", icon: ArrowLeftRight },
  { label: "Deployments", href: "/deployments", icon: Rocket },
  { label: "Audit Log", href: "/audit", icon: ScrollText },
  { label: "Customer Simulator", href: "/simulator", icon: FlaskConical },
  { label: "Settings", href: "/settings", icon: Settings },
];
export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [mobile, setMobile] = useState(false);
  const [search, setSearch] = useState("");
  const [settledSearch, setSettledSearch] = useState("");
  const [signOutError, setSignOutError] = useState("");
  const { environment, setEnvironment } = useEnvironment();
  const { session, logout } = useSession();
  useEffect(() => {
    const timer = window.setTimeout(() => setSettledSearch(search.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [search]);
  const searchQuery = useQuery({
    queryKey: ["workspace-search", environment, settledSearch],
    queryFn: () => workspaceSearch(environment, settledSearch),
    enabled: open && settledSearch.length >= 2,
    refetchOnWindowFocus: false,
  });
  const searching = search.trim() !== settledSearch || (settledSearch.length >= 2 && searchQuery.isPending);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "k") {
        event.preventDefault();
        setOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);
  const go = (href: string) => {
    setOpen(false);
    setMobile(false);
    router.push(href);
  };
  if (session.kind !== "local" && session.kind !== "authenticated") {
    return <SessionScreen />;
  }
  const user = session.kind === "authenticated" ? session.user : null;
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="topbar">
        <Link href="/" className="brand">
          <Activity aria-hidden="true" />
          <span>
            Telco<span className="brand-light">Pulse</span>
            <small>Observe. Resolve. Stay connected.</small>
          </span>
        </Link>
        <Button
          variant="ghost"
          size="icon"
          className="mobile-menu"
          aria-label="Open navigation"
          onClick={() => setMobile(true)}
        >
          <Menu />
        </Button>
        <button className="global-search" onClick={() => setOpen(true)}>
          <Search size={16} />
          <span>Search the workspace or jump to a page...</span>
          <kbd>⌘ K</kbd>
        </button>
        <div className="header-controls">
          <OperationsInbox environment={environment} />
          <NativeSelect
            aria-label="Environment"
            value={environment}
            onChange={(e) =>
              setEnvironment(environmentSchema.parse(e.target.value))
            }
          >
            <NativeSelectOption value="development">
              Development
            </NativeSelectOption>
            <NativeSelectOption value="staging">Staging</NativeSelectOption>
          </NativeSelect>
          <span className="internal-label">
            <ShieldCheck size={14} /> Internal workspace
          </span>
          <div className="profile">
            <span className="avatar">
              {user ? user.username.slice(0, 2).toUpperCase() : "LO"}
            </span>
            <span>
              {user?.username ?? "Local operator"}
              <small>{user?.role ?? "Development access"}</small>
            </span>
          </div>
          {user && (
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Sign out"
              title="Sign out"
              onClick={() => {
                setSignOutError("");
                void logout().catch(() =>
                  setSignOutError("Sign-out is unavailable. Try again."),
                );
              }}
            >
              <LogOut aria-hidden="true" />
            </Button>
          )}
        </div>
      </header>
      {signOutError && (
        <div className="session-signout-error" role="alert">
          {signOutError}
        </div>
      )}
      <aside className="sidebar">
        <div className="workspace-label">ITOC WORKSPACE</div>
        <nav aria-label="Main navigation">
          {navigation.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "nav-item",
                (item.href === "/"
                  ? path === "/"
                  : path.startsWith(item.href)) && "active",
              )}
              aria-current={
                (item.href === "/" ? path === "/" : path.startsWith(item.href))
                  ? "page"
                  : undefined
              }
            >
              <item.icon size={18} />
              {item.label}
              {(item.href === "/"
                ? path === "/"
                : path.startsWith(item.href)) && (
                <ChevronRight size={14} className="nav-chevron" />
              )}
            </Link>
          ))}
        </nav>
        <div className="sidebar-footer">
          <Radio size={22} />
          <p>
            Keeping connectivity
            <br />
            brighter, together.
          </p>
          <div>
            <span>Local environment</span>
            <span>v0.2.0</span>
          </div>
        </div>
      </aside>
      <main id="main" className="main-content" tabIndex={-1}>
        {children}
        <footer className="page-footer">
          <span>
            <ShieldCheck size={12} /> Fictional telecom operations · Synthetic
            data only
          </span>
          <span>Asia/Jakarta · UTC+7</span>
        </footer>
      </main>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Command size={18} /> Search workspace
            </DialogTitle>
            <DialogDescription>
              Find an incident, transaction, deployment or simulation in {environment}.
            </DialogDescription>
          </DialogHeader>
          <Input
            autoFocus
            aria-label="Search workspace"
            placeholder="ID, trace ID, incident title or service"
            maxLength={80}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !searching && searchQuery.data?.items[0]) {
                e.preventDefault();
                go(workspaceHitPath(searchQuery.data.items[0]));
              }
            }}
          />
          <div className="command-list">
            {navigation
              .filter((n) =>
                n.label.toLowerCase().includes(search.toLowerCase()),
              )
              .map((n) => (
                <Button key={n.href} variant="ghost" onClick={() => go(n.href)}>
                  <n.icon data-icon="inline-start" />
                  {n.label}
                  <ArrowUpRight data-icon="inline-end" />
                </Button>
              ))}
            {search.trim().length >= 2 && <div className="workspace-results" aria-live="polite">
              {searching ? <p className="muted">Searching workspace…</p> : searchQuery.isError ? <div className="workspace-search-state"><p>Search is unavailable. Try again.</p><Button variant="outline" size="sm" onClick={() => void searchQuery.refetch()}>Retry search</Button></div> : searchQuery.data?.items.length === 0 ? <p className="muted">No matching records in {environment}.</p> : searchQuery.data?.items.map((hit) =>
                <Button key={`${hit.source}:${hit.resource_id}`} variant="ghost" className="workspace-result" onClick={() => go(workspaceHitPath(hit))}>
                  <span className="workspace-result-main"><strong>{hit.title}</strong><small className="muted">{hit.detail}</small></span>
                  <Badge variant="outline" className="status-neutral">{hit.source}</Badge>
                </Button>
              )}
            </div>}
            {search.trim() && <Button variant="outline" onClick={() => go(`/transactions?search=${encodeURIComponent(search.trim())}`)}>Open transaction explorer for “{search.trim()}”</Button>}
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={mobile} onOpenChange={setMobile}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>TelcoPulse navigation</DialogTitle>
            <DialogDescription>Open an operations workspace.</DialogDescription>
          </DialogHeader>
          <div className="command-list">
            {navigation.map((n) => (
              <Button key={n.href} variant="ghost" onClick={() => go(n.href)}>
                <n.icon />
                {n.label}
              </Button>
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
