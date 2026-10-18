"use client";

import * as React from "react";
import { useSearchParams } from "next/navigation";
import { CheckCircle2, TriangleAlert } from "lucide-react";
import { OAUTH_CHANNEL, type OAuthResult } from "@/lib/credentials";

/**
 * Landing page of the OAuth popup (the API's /api/oauth/callback redirects
 * here). It hands the result to the builder tab that opened it and closes.
 */
function Done() {
  const params = useSearchParams();
  const status = params.get("status") === "connected" ? "connected" : "error";
  const message = params.get("message") ?? undefined;

  React.useEffect(() => {
    const result: OAuthResult = {
      type: "agently-oauth",
      status,
      credentialId: params.get("credentialId") ?? undefined,
      message,
    };
    const channel = new BroadcastChannel(OAUTH_CHANNEL);
    channel.postMessage(result);
    channel.close();
    if (status === "connected") {
      const t = window.setTimeout(() => window.close(), 600);
      return () => window.clearTimeout(t);
    }
  }, [params, status, message]);

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg px-6">
      <div className="max-w-sm text-center">
        {status === "connected" ? (
          <>
            <CheckCircle2 className="mx-auto size-10 text-success" />
            <h1 className="mt-3 text-[17px] font-semibold text-fg">Account connected</h1>
            <p className="mt-1 text-[13px] text-muted">You can close this window.</p>
          </>
        ) : (
          <>
            <TriangleAlert className="mx-auto size-10 text-danger" />
            <h1 className="mt-3 text-[17px] font-semibold text-fg">Sign-in didn't finish</h1>
            <p className="mt-1 text-[13px] text-muted break-words">{message || "Something went wrong."}</p>
            <button
              onClick={() => window.close()}
              className="mt-4 text-[13px] text-accent hover:text-accent-soft"
            >
              Close window
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export default function OAuthDonePage() {
  return (
    <React.Suspense fallback={null}>
      <Done />
    </React.Suspense>
  );
}
