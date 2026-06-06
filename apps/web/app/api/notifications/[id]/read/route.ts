import type { NotificationId } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { ok, handle, asId } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function POST(_req: Request, ctx: { params: Promise<{ id: string }> }) {
  return handle(async () => {
    const { id } = await ctx.params;
    return ok(platform.notifications.markRead(asId<NotificationId>(id)));
  });
}
