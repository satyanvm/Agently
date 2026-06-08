import { Sidebar } from "@/components/shell/sidebar";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="relative min-h-screen">
      <Sidebar />
      {/* min-w-0 + overflow-x-clip keep the page from scrolling sideways when a
          wide child (e.g. the agent graph) appears — it scrolls internally. */}
      <div className="relative z-10 min-w-0 overflow-x-clip pl-[244px]">{children}</div>
    </div>
  );
}
