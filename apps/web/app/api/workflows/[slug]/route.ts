import { platform } from "@/lib/server/platform";
import { ok, handle } from "@/lib/server/http";

export const dynamic = "force-dynamic";

export function GET(_req: Request, ctx: { params: Promise<{ slug: string }> }) {
  return handle(async () => {
    const { slug } = await ctx.params;
    return ok(platform.workflows.getBySlug(slug));
  });
}
