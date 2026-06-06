import { createClient, type SupabaseClient } from "@supabase/supabase-js";

/**
 * Two distinct clients, by design:
 *
 *  - Anon client: respects Row Level Security. Used in the browser and in
 *    user-facing server routes, scoped to the logged-in user.
 *  - Service client: bypasses RLS via the service-role key. Used ONLY by the
 *    worker and trusted server code. Never ship the service key to the browser.
 *
 * The service client is created lazily so that importing this module in the
 * browser bundle never touches the service-role key.
 */

let serviceClient: SupabaseClient | null = null;

export function getServiceClient(): SupabaseClient {
  if (serviceClient) return serviceClient;

  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.SUPABASE_SERVICE_ROLE_KEY;
  if (!url || !key) {
    throw new Error(
      "getServiceClient: NEXT_PUBLIC_SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY must be set",
    );
  }

  serviceClient = createClient(url, key, {
    auth: { persistSession: false, autoRefreshToken: false },
  });
  return serviceClient;
}

export type { SupabaseClient };
