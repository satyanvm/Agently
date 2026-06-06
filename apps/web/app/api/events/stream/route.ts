import { platform } from "@/lib/server/platform";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

/**
 * Workspace-wide domain event stream (SSE). Powers live dashboard counters,
 * toast notifications, and the activity feed without polling. A reconnecting
 * client can pass `Last-Event-ID` to replay missed events from the bus buffer.
 */
export async function GET(req: Request) {
  const encoder = new TextEncoder();
  const lastEventId = req.headers.get("last-event-id");

  const stream = new ReadableStream({
    start(controller) {
      const frame = (evt: { id: string; type: string }) =>
        controller.enqueue(
          encoder.encode(`id: ${evt.id}\nevent: ${evt.type}\ndata: ${JSON.stringify(evt)}\n\n`),
        );

      // Replay anything missed since the client's last seen event.
      for (const evt of platform.bus.replayAfter(lastEventId)) frame(evt);

      const unsubscribe = platform.bus.subscribe(frame);
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
