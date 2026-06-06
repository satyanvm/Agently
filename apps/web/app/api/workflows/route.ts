import { PageQuery, CreateWorkflowInput } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { ok, handle, parseQuery, parseBody } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET(req: Request) {
  return handle(() => {
    const query = parseQuery(PageQuery, req.url);
    return ok(platform.workflows.list(query));
  });
}

export function POST(req: Request) {
  return handle(async () => {
    const input = await parseBody(CreateWorkflowInput, req);
    return ok(platform.workflows.create(input), 201);
  });
}
