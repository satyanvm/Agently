import { LaunchRunInput } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { ok, handle, parseBody } from "@/lib/server/http";

export const dynamic = "force-dynamic";

/** Launch a new run of the workflow. */
export function POST(req: Request, ctx: { params: Promise<{ slug: string }> }) {
  return handle(async () => {
    const { slug } = await ctx.params;
    const input = await parseBody(LaunchRunInput, req);
    return ok(platform.runs.launch(slug, input), 201);
  });
}

/** Runs for this workflow (newest first). */
export function GET(_req: Request, ctx: { params: Promise<{ slug: string }> }) {
  return handle(async () => {
    const { slug } = await ctx.params;
    const wf = platform.workflows.getBySlug(slug);
    return ok(platform.runs.list({ limit: 50, workflowId: wf.id }));
  });
}
