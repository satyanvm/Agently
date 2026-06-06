import { DomainError, type RunId } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { ok, handle, asId } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET(_req: Request, ctx: { params: Promise<{ id: string }> }) {
  return handle(async () => {
    const { id } = await ctx.params;
    const session = platform.browser.getByRun(asId<RunId>(id));
    if (!session) throw DomainError.notFound("browser session");
    return ok(session);
  });
}
