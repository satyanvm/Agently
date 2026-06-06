import { platform } from "@/lib/server/platform";
import { ok, handle } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET() {
  return handle(() => ok(platform.stats.workspace()));
}
