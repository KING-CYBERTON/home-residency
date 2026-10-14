import { Authenticated, Refine } from "@refinedev/core";
import routerProvider, { CatchAllNavigate } from "@refinedev/react-router";
import { MutationCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Outlet, Route, Routes, useSearchParams } from "react-router";

import { setUnauthenticatedHandler } from "@/api/client";
import { Guard, Layout } from "@/components/layout";
import { Toaster } from "@/components/ui/sonner";
import { accessControlProvider } from "@/providers/access";
import { authProvider, safeReturnPath } from "@/providers/auth";
import { dataProvider } from "@/providers/data";
import { notificationProvider } from "@/providers/notification";
import { nav } from "@/resources";
import { routes } from "@/routes";
import { LoginPage } from "@/pages/login";

// One cache for Refine's hooks and the typed openapi-react-query hooks.
// Every successful write refreshes what is on screen: console actions move
// work between queues, so stale lists are worse than an extra request.
const queryClient: QueryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true, staleTime: 5_000 } },
  mutationCache: new MutationCache({
    onSuccess: () => queryClient.invalidateQueries(),
  }),
});

setUnauthenticatedHandler(() => {
  queryClient.clear();
  if (!window.location.pathname.startsWith("/login")) window.location.assign("/login");
});

/** A signed-in user who opens /login goes back to where they came from. */
function BackToConsole() {
  const [params] = useSearchParams();
  return <Navigate to={safeReturnPath(params.get("to"))} replace />;
}

export default function App() {
  return (
    <BrowserRouter>
      <QueryClientProvider client={queryClient}>
        <Refine
          routerProvider={routerProvider}
          dataProvider={dataProvider}
          authProvider={authProvider}
          accessControlProvider={accessControlProvider}
          notificationProvider={notificationProvider}
          resources={nav.map((n) => ({ name: n.name, list: n.route, meta: { label: n.label, op: n.op, parent: n.group || undefined } }))}
          options={{
            reactQuery: { clientConfig: queryClient },
            syncWithLocation: false,
            disableTelemetry: true,
            warnWhenUnsavedChanges: false,
          }}
        >
          <Routes>
            <Route
              element={
                <Authenticated key="console" fallback={<CatchAllNavigate to="/login" />}>
                  <Layout>
                    <Outlet />
                  </Layout>
                </Authenticated>
              }
            >
              {routes.map((r) => (
                <Route key={r.path} path={r.path} element={<Guard op={r.op}>{r.element}</Guard>} />
              ))}
            </Route>
            <Route
              element={
                <Authenticated key="login" fallback={<Outlet />}>
                  <BackToConsole />
                </Authenticated>
              }
            >
              <Route path="/login" element={<LoginPage />} />
            </Route>
          </Routes>
          <Toaster position="bottom-right" />
        </Refine>
      </QueryClientProvider>
    </BrowserRouter>
  );
}
