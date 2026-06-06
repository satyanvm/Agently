import { ListLogsQuery, type RunId } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { ok, handle, parseQuery, asId } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET(req: Request, ctx: { params: Promise<{ id: string }> }) {
  return handle(async () => {
    const { id } = await ctx.params;
    const query = parseQuery(ListLogsQuery, req.url);
    return ok(platform.logs.list(asId<RunId>(id), query));
  });
}
