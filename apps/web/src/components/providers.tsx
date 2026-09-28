"use client";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import {
  QueryClient,
  QueryClientProvider,
  useQueryClient,
} from "@tanstack/react-query";
import { Toaster } from "sonner";
import type { Environment } from "@/lib/contracts";
import { APIError, SESSION_EXPIRED_EVENT } from "@/lib/api";
import {
  sessionAPI,
  canCreateIncident,
  canEditIncident,
  canEscalateIncident,
  canGeneratePostmortem,
  canInjectFailure,
  canReplayDeadLetter,
  canRunPurchase,
  type Operator,
} from "@/lib/session";

type SessionState =
  | { kind: "loading" | "local" | "anonymous" | "unavailable" }
  | { kind: "authenticated"; user: Operator };
type SessionContextValue = {
  session: SessionState;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  retry: () => Promise<void>;
};
const SessionContext = createContext<SessionContextValue | null>(null);
export function useSession() {
  const value = useContext(SessionContext);
  if (!value) throw new Error("SessionProvider is missing");
  return value;
}
export function usePermission(
  action: "createIncident" | "editIncident" | "escalateIncident" | "generatePostmortem" | "injectFailure" | "runPurchase" | "replayDeadLetter",
) {
  const { session } = useSession();
  if (session.kind === "local") return true;
  if (session.kind !== "authenticated") return false;
  switch (action) {
    case "createIncident":
      return canCreateIncident(session.user.role);
    case "editIncident":
      return canEditIncident(session.user.role);
    case "escalateIncident":
      return canEscalateIncident(session.user.role);
    case "generatePostmortem":
      return canGeneratePostmortem(session.user.role);
    case "injectFailure":
      return canInjectFailure(session.user.role);
    case "runPurchase":
      return canRunPurchase(session.user.role);
    case "replayDeadLetter":
      return canReplayDeadLetter(session.user.role);
  }
}

function SessionProvider({ children }: { children: React.ReactNode }) {
  const client = useQueryClient();
  const [session, setSession] = useState<SessionState>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      const mode = await sessionAPI.status();
      if (!mode.required) {
        setSession({ kind: "local" });
        return;
      }
      const user = await sessionAPI.me();
      setSession({ kind: "authenticated", user });
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        client.clear();
        setSession({ kind: "anonymous" });
      } else {
        setSession({ kind: "unavailable" });
      }
    }
  }, [client]);
  const retry = async () => {
    setSession({ kind: "loading" });
    await load();
  };
  useEffect(() => {
    queueMicrotask(() => {
      void load();
    });
  }, [load]);
  useEffect(() => {
    const expire = () => {
      client.clear();
      setSession({ kind: "anonymous" });
    };
    window.addEventListener(SESSION_EXPIRED_EVENT, expire);
    return () => window.removeEventListener(SESSION_EXPIRED_EVENT, expire);
  }, [client]);
  useEffect(() => {
    if (session.kind !== "authenticated") return;
    let active = true;
    const revalidate = () => {
      void sessionAPI
        .me()
        .then((user) => {
          if (active) setSession({ kind: "authenticated", user });
        })
        .catch((error) => {
          if (!active) return;
          client.clear();
          setSession(
            error instanceof APIError && error.status === 401
              ? { kind: "anonymous" }
              : { kind: "unavailable" },
          );
        });
    };
    const interval = window.setInterval(revalidate, 60_000);
    window.addEventListener("focus", revalidate);
    return () => {
      active = false;
      window.clearInterval(interval);
      window.removeEventListener("focus", revalidate);
    };
  }, [client, session.kind]);
  const login = async (username: string, password: string) => {
    const user = await sessionAPI.login(username, password);
    client.clear();
    setSession({ kind: "authenticated", user });
  };
  const logout = async () => {
    await sessionAPI.logout();
    client.clear();
    setSession({ kind: "anonymous" });
  };
  return (
    <SessionContext.Provider value={{ session, login, logout, retry }}>
      {children}
    </SessionContext.Provider>
  );
}
const EnvironmentContext = createContext<{
  environment: Environment;
  setEnvironment: (v: Environment) => void;
}>({ environment: "development", setEnvironment: () => {} });
export const useEnvironment = () => useContext(EnvironmentContext);
export function Providers({ children }: { children: React.ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 5000, retry: 1, refetchInterval: 15000 },
          mutations: { retry: false },
        },
      }),
  );
  const [environment, setEnvironment] = useState<Environment>("development");
  return (
    <QueryClientProvider client={client}>
      <SessionProvider>
        <EnvironmentContext.Provider value={{ environment, setEnvironment }}>
          {children}
          <Toaster theme="dark" position="bottom-right" richColors closeButton />
        </EnvironmentContext.Provider>
      </SessionProvider>
    </QueryClientProvider>
  );
}
