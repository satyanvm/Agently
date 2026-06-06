import { ok } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET() {
  return ok({ ok: true, service: "agently-web", time: new Date().toISOString() });
}
