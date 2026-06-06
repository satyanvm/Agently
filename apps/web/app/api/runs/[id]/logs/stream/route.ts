import { isEvent, type RunId } from "@agently/contracts";
import { platform } from "@/lib/server/platform";
import { asId } from "@/lib/server/http";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

/**
 * Server-Sent Events stream of new log lines for a run. The frontend's log
 * viewer simulates streaming today; this is the real endpoint it can switch to
 * with no UI change — same `LogEntry` shape, pushed as it's appended.
 *
 * Transport is SSE for simplicity; swapping to WebSocket later is isolated to
 * this file because the event source is the abstract {@link platform.bus}.
 */
export async function GET(req: Request, ctx: { params: Promise<{ id: string }> }) {
  const { id } = await ctx.params;
  const runId = asId<RunId>(id);
  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    start(controller) {
      const send = (event: string, data: unknown) =>
        controller.enqueue(encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`));

      send("ready", { runId });

      const unsubscribe = platform.bus.subscribe((evt) => {
        if (isEvent(evt, "run.log.appended") && evt.runId === runId) {
          send("log", evt.log);
        } else if (isEvent(evt, "run.finished") && evt.runId === runId) {
          send("finished", { status: evt.status });
        }
      });

      const heartbeat = setInterval(() => controller.enqueue(encoder.encode(": keep-alive\n\n")), 15000);

      req.signal.addEventListener("abort", () => {
        clearInterval(heartbeat);
        unsubscribe();
        try {
          controller.close();
        } catch {
          /* already closed */
        }
      });
    },
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
    },
  });
}
