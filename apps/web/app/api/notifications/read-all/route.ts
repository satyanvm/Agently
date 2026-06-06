import { platform } from "@/lib/server/platform";
import { ok, handle } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function POST() {
  return handle(() => ok(platform.notifications.markAllRead()));
}
