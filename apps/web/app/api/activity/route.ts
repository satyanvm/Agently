import { PageQuery } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { ok, handle, parseQuery } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET(req: Request) {
  return handle(() => {
    const query = parseQuery(PageQuery, req.url);
    return ok(platform.activity.list(query));
  });
}
