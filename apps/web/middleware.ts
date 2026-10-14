import { NextResponse, type NextRequest } from "next/server";

/**
 * Access control for a hosted deployment.
 *
 * - AGENTLY_WEB_PASSWORD: when set, every page and /api call needs HTTP Basic
 *   auth with that password (any username). Basic auth rides along on
 *   EventSource streams and artifact downloads, which a header-based scheme
 *   would not.
 * - AGENTLY_API_TOKEN: attached server-side to every proxied /api request so
 *   the Go API accepts it. The browser never sees the token.
 *
 * Webhook ingress (/api/hooks/*) is exempt: third-party providers call it.
 */
export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  const password = process.env.AGENTLY_WEB_PASSWORD;
  const isHook = pathname.startsWith("/api/hooks/");

  if (password && !isHook && !basicAuthOk(req.headers.get("authorization"), password)) {
    return new NextResponse("Authentication required", {
      status: 401,
      headers: { "WWW-Authenticate": 'Basic realm="Agently", charset="UTF-8"' },
    });
  }

  if (!pathname.startsWith("/api/")) return NextResponse.next();

  const headers = new Headers(req.headers);
  const token = process.env.AGENTLY_API_TOKEN;
  if (token) headers.set("authorization", `Bearer ${token}`);
  else headers.delete("authorization");
  return NextResponse.next({ request: { headers } });
}

function basicAuthOk(header: string | null, password: string): boolean {
  if (!header?.startsWith("Basic ")) return false;
  let decoded: string;
  try {
    decoded = atob(header.slice(6));
  } catch {
    return false;
  }
  const given = decoded.slice(decoded.indexOf(":") + 1);
  return safeEqual(given, password);
}

function safeEqual(a: string, b: string): boolean {
  let diff = a.length ^ b.length;
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    diff |= (a.charCodeAt(i) || 0) ^ (b.charCodeAt(i) || 0);
  }
  return diff === 0;
}

export const config = {
  runtime: "nodejs",
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
