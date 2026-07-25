"use client";

import * as React from "react";
import { listCredentials, type CredentialSummary } from "@/lib/credentials";

/**
 * Workspace credentials, fetched once and shared by the canvas (needs-creds
 * badge on every node card) and the inspector (credential picker). Mutations
 * (create/update/delete) call refresh() to keep both in sync.
 */
interface CredentialsState {
  /** null until the first successful load (loading or API unreachable). */
  credentials: CredentialSummary[] | null;
  loading: boolean;
  /** Non-null when the last load failed — the builder stays usable. */
  error: string | null;
  refresh: () => Promise<void>;
}

const CredentialsContext = React.createContext<CredentialsState>({
  credentials: null,
  loading: false,
  error: null,
  refresh: async () => {},
});

export function useCredentials(): CredentialsState {
  return React.useContext(CredentialsContext);
}

export function CredentialsProvider({ children }: { children: React.ReactNode }) {
  const [credentials, setCredentials] = React.useState<CredentialSummary[] | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);

  const refresh = React.useCallback(async () => {
    setLoading(true);
    try {
      setCredentials(await listCredentials());
      setError(null);
    } catch (e) {
      // Non-blocking: keep any previously loaded list; the builder still works.
      setError(e instanceof Error ? e.message : "Failed to load credentials");
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const value = React.useMemo(
    () => ({ credentials, loading, error, refresh }),
    [credentials, loading, error, refresh],
  );

  return <CredentialsContext.Provider value={value}>{children}</CredentialsContext.Provider>;
}
