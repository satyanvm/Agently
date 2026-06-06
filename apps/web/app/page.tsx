import Link from "next/link";
import {
  ArrowRight,
  Plug,
  Network,
  Globe,
  Terminal,
  Gauge,
  Bell,
  ShieldCheck,
  Workflow as WorkflowIcon,
  Eye,
  Sparkles,
} from "lucide-react";
import { StatusBadge, StatusDot } from "@/components/ui/status";
import { LivePill } from "@/components/live";

export default function LandingPage() {
  return (
    <div className="relative overflow-x-clip">
      <LandingNav />
      <Hero />
      <LogoCloud />
      <Problem />
      <HowItWorks />
      <Features />
      <Metrics />
      <CTA />
      <Footer />
    </div>
  );
}

/* ----------------------------------------------------------------- Nav */
function LandingNav() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/60 bg-bg/70 backdrop-blur-xl">
      <div className="mx-auto flex h-15 max-w-[1180px] items-center gap-8 px-6 py-3.5">
        <Link href="/" className="flex items-center gap-2">
          <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-accent to-accent-soft shadow-[0_4px_14px_-2px_rgba(79,57,230,0.5)]">
            <Star className="h-4 w-4 text-white" />
          </span>
          <span className="text-[15px] font-semibold tracking-tight">Agently</span>
        </Link>
        <nav className="hidden items-center gap-6 text-[13px] text-muted md:flex">
          <a className="transition-colors hover:text-fg" href="#product">Product</a>
          <a className="transition-colors hover:text-fg" href="#how">How it works</a>
          <a className="transition-colors hover:text-fg" href="#features">Features</a>
          <a className="transition-colors hover:text-fg" href="#">Pricing</a>
          <a className="transition-colors hover:text-fg" href="#">Docs</a>
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <Link
            href="/dashboard"
            className="hidden h-9 items-center rounded-md px-3 text-[13px] font-medium text-muted transition-colors hover:text-fg sm:flex"
          >
            Sign in
          </Link>
          <Link
            href="/dashboard"
            className="flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-[13px] font-medium text-accent-fg transition-colors hover:bg-accent-soft glow-accent"
          >
            Open console
            <ArrowRight className="size-3.5" />
          </Link>
        </div>
      </div>
    </header>
  );
}

/* ---------------------------------------------------------------- Hero */
function Hero() {
  return (
    <section className="relative">
      <div className="bg-grid pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_60%_50%_at_50%_0%,black,transparent)]" />
      <div className="relative mx-auto max-w-[1180px] px-6 pb-10 pt-20 text-center md:pt-28">
        <div className="animate-in mx-auto mb-6 inline-flex items-center gap-2 rounded-full border border-border bg-surface/60 px-3 py-1 text-[12px] text-muted">
          <Sparkles className="size-3.5 text-accent-soft" />
          The cloud for autonomous agents
          <span className="text-ghost">·</span>
          <span className="text-faint">v2 is live</span>
        </div>

        <h1 className="animate-in mx-auto max-w-[840px] text-balance text-[46px] font-bold leading-[1.02] tracking-[-0.03em] md:text-[68px]">
          <span className="text-gradient">Deploy agents that keep</span>{" "}
          <span className="text-gradient-accent">running after you leave.</span>
        </h1>

        <p className="animate-in mx-auto mt-6 max-w-[600px] text-balance text-[16px] leading-relaxed text-muted md:text-[17px]">
          Agently is the execution platform for long-running, multi-agent
          workflows. Launch a crew, close your laptop, and come back to finished
          work — with live logs, browser activity, cost and runtime in one
          control plane.
        </p>

        <div className="animate-in mt-9 flex flex-col items-center justify-center gap-3 sm:flex-row">
          <Link
            href="/dashboard"
            className="group flex h-11 items-center gap-2 rounded-lg bg-accent px-5 text-[15px] font-medium text-accent-fg transition-colors hover:bg-accent-soft glow-accent"
          >
            Start building
            <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </Link>
          <Link
            href="/workflows/competitive-intelligence-sweep"
            className="flex h-11 items-center gap-2 rounded-lg border border-border bg-surface/60 px-5 text-[15px] font-medium text-fg transition-colors hover:border-border-strong"
          >
            <Eye className="size-4" />
            See a live workflow
          </Link>
        </div>
        <p className="animate-in mt-4 text-[12px] text-faint">
          No credit card · 100 agent-hours free · SOC 2 Type II
        </p>

        <HeroPreview />
      </div>
    </section>
  );
}

/* Faux product window — gives the page its "this is real" weight. */
function HeroPreview() {
  return (
    <div className="animate-in relative mx-auto mt-16 max-w-[1000px]">
      <div className="absolute -inset-x-10 -top-10 bottom-0 -z-10 bg-[radial-gradient(60%_60%_at_50%_0%,rgba(79,57,230,0.18),transparent_70%)] blur-2xl" />
      <div className="overflow-hidden rounded-xl border border-border-strong bg-surface shadow-pop">
        {/* window chrome */}
        <div className="flex items-center gap-2 border-b border-border bg-surface-2/60 px-4 py-2.5">
          <span className="h-2.5 w-2.5 rounded-full bg-[#ff5f57]/80" />
          <span className="h-2.5 w-2.5 rounded-full bg-[#febc2e]/80" />
          <span className="h-2.5 w-2.5 rounded-full bg-[#28c840]/80" />
          <div className="ml-3 flex items-center gap-1.5 rounded-md bg-surface px-2.5 py-1 text-[11px] text-faint">
            <ShieldCheck className="size-3 text-success" />
            agently.dev/runs/142
          </div>
          <div className="ml-auto"><LivePill /></div>
        </div>
        {/* body */}
        <div className="grid grid-cols-12 gap-px bg-border text-left">
          {/* mini sidebar */}
          <div className="col-span-3 hidden flex-col gap-1 bg-surface p-3 md:flex">
            {["Dashboard", "Workflows", "Runs", "Agents", "Browser"].map((l, i) => (
              <div
                key={l}
                className={`flex items-center gap-2 rounded-md px-2 py-1.5 text-[12px] ${
                  i === 2 ? "bg-surface-2 text-fg" : "text-faint"
                }`}
              >
                <span className={`h-2 w-2 rounded-sm ${i === 2 ? "bg-accent" : "bg-ghost"}`} />
                {l}
              </div>
            ))}
          </div>
          {/* main */}
          <div className="col-span-12 bg-surface p-4 md:col-span-9">
            <div className="mb-3 flex items-center justify-between">
              <div>
                <div className="text-[13px] font-semibold">Competitive Intelligence Sweep</div>
                <div className="text-[11px] text-faint">Run #142 · 7 agents · us-east-1</div>
              </div>
              <StatusBadge status="running" />
            </div>
            {/* agent rail */}
            <div className="mb-3 grid grid-cols-4 gap-2">
              {[
                { n: "Conductor", s: "running" as const },
                { n: "Scout · Pricing", s: "succeeded" as const },
                { n: "Navigator", s: "running" as const },
                { n: "Synthesizer", s: "waiting" as const },
              ].map((a) => (
                <div key={a.n} className="rounded-lg border border-border bg-surface-2/50 p-2.5">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="truncate text-[11px] font-medium">{a.n}</span>
                    <StatusDot status={a.s} />
                  </div>
                  <div className="h-1 w-full overflow-hidden rounded-full bg-surface-3">
                    <div
                      className="h-full rounded-full bg-accent"
                      style={{ width: a.s === "succeeded" ? "100%" : a.s === "waiting" ? "12%" : "64%" }}
                    />
                  </div>
                </div>
              ))}
            </div>
            {/* faux log */}
            <div className="rounded-lg border border-border bg-inset p-3 font-mono text-[10.5px] leading-relaxed">
              <LogRow t="17:09:12" c="text-faint" s="system" m="Sandbox ready · 4 vCPU / 8 GiB" />
              <LogRow t="17:09:52" c="text-blue-600" s="Conductor" m="→ delegated 3 scouts" />
              <LogRow t="17:11:31" c="text-emerald-600" s="Navigator" m="screenshot captured · acme-pricing.png" />
              <LogRow t="17:14:30" c="text-success" s="Scout·Pricing" m="completed · 11/11 rows" />
              <LogRow t="17:18:10" c="text-warn" s="Synthesizer" m="price conflict flagged for audit" />
              <div className="flex gap-2 text-fg">
                <span className="text-ghost">17:31:11</span>
                <span className="text-accent">Composer</span>
                <span>drafting executive brief<span className="caret text-accent">▍</span></span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function LogRow({ t, c, s, m }: { t: string; c: string; s: string; m: string }) {
  return (
    <div className="flex gap-2">
      <span className="text-ghost">{t}</span>
      <span className={c}>{s}</span>
      <span className="text-muted">{m}</span>
    </div>
  );
}

/* ------------------------------------------------------------ Logo cloud */
function LogoCloud() {
  return (
    <section className="border-y border-border/50 bg-surface/20 py-10">
      <div className="mx-auto max-w-[1180px] px-6">
        <p className="mb-7 text-center text-[12px] font-medium uppercase tracking-wider text-faint">
          Trusted to run autonomous work at
        </p>
        <div className="flex flex-wrap items-center justify-center gap-x-12 gap-y-6 opacity-70">
          {["Northwind", "Helix", "Quanta", "Vantage", "Orbital", "Meridian"].map((b) => (
            <span key={b} className="text-[18px] font-semibold tracking-tight text-muted">
              {b}
            </span>
          ))}
        </div>
      </div>
    </section>
  );
}

/* -------------------------------------------------------------- Problem */
function Problem() {
  return (
    <section id="product" className="mx-auto max-w-[1180px] px-6 py-24">
      <div className="grid items-center gap-12 md:grid-cols-2">
        <div>
          <span className="text-[12px] font-semibold uppercase tracking-wider text-accent-soft">
            The problem
          </span>
          <h2 className="mt-3 text-balance text-[32px] font-semibold leading-tight tracking-tight md:text-[38px]">
            Agents are powerful. The place they run is not.
          </h2>
          <p className="mt-5 text-[15px] leading-relaxed text-muted">
            Today, autonomous agents live inside a notebook, a terminal tab, or a
            script on your laptop. The moment you disconnect, they die. There&apos;s
            no durable execution, no visibility into what a 7-agent crew is doing,
            no record of which browser it drove or what it spent.
          </p>
          <ul className="mt-6 space-y-3">
            {[
              "Long jobs stop the second the tab closes",
              "No way to watch a multi-agent crew coordinate",
              "Browser steps fail silently, with no replay",
              "Cost and runtime are a mystery until the bill arrives",
            ].map((t) => (
              <li key={t} className="flex items-start gap-2.5 text-[14px] text-muted">
                <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-danger" />
                {t}
              </li>
            ))}
          </ul>
        </div>

        <div className="relative">
          <div className="rounded-xl border border-border bg-surface p-6 shadow-card">
            <div className="mb-4 flex items-center justify-between">
              <span className="text-[13px] font-medium text-muted">Without a platform</span>
              <span className="rounded bg-danger-bg px-2 py-0.5 text-[11px] text-danger">disconnected</span>
            </div>
            <div className="space-y-2 font-mono text-[12px]">
              <div className="text-faint">$ python run_agents.py</div>
              <div className="text-muted">[agent] researching competitors…</div>
              <div className="text-muted">[agent] browsing acme.dev…</div>
              <div className="text-danger">✗ Terminated (SIGHUP) — session closed</div>
              <div className="text-ghost">work lost · no logs · no artifacts</div>
            </div>
          </div>
          <div className="mt-4 rounded-xl border border-accent/30 bg-accent-bg/40 p-6 ring-1 ring-accent/20">
            <div className="mb-4 flex items-center justify-between">
              <span className="text-[13px] font-medium text-fg">With Agently</span>
              <LivePill label="still running" />
            </div>
            <div className="space-y-2 font-mono text-[12px]">
              <div className="text-faint">$ agently deploy ./crew</div>
              <div className="text-success">✓ Crew running in us-east-1 · detached</div>
              <div className="text-muted">7 agents · live logs · browser capture</div>
              <div className="text-accent-soft">→ notified when complete</div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

/* ------------------------------------------------------------ How it works */
function HowItWorks() {
  const steps = [
    {
      icon: WorkflowIcon,
      title: "Define your workflow",
      body: "Describe a crew of agents, their dependencies, and the tools they can use. Version every change.",
    },
    {
      icon: Plug,
      title: "Deploy & detach",
      body: "We provision an isolated sandbox per run and execute durably. Close your laptop — it keeps going.",
    },
    {
      icon: Gauge,
      title: "Monitor live",
      body: "Watch the agent graph coordinate, stream logs, scrub browser sessions, and track cost in real time.",
    },
    {
      icon: Bell,
      title: "Get the result",
      body: "Artifacts are collected and you're notified the moment a run completes, fails, or needs a human.",
    },
  ];
  return (
    <section id="how" className="border-y border-border/50 bg-surface/20 py-24">
      <div className="mx-auto max-w-[1180px] px-6">
        <div className="mb-14 text-center">
          <span className="text-[12px] font-semibold uppercase tracking-wider text-accent-soft">
            How it works
          </span>
          <h2 className="mt-3 text-[32px] font-semibold tracking-tight md:text-[38px]">
            From prompt to finished work, durably.
          </h2>
        </div>
        <div className="grid gap-5 md:grid-cols-4">
          {steps.map((s, i) => {
            const Icon = s.icon;
            return (
              <div
                key={s.title}
                className="relative rounded-xl border border-border bg-surface p-5 shadow-card"
              >
                <span className="absolute right-4 top-4 font-mono text-[12px] text-ghost">
                  0{i + 1}
                </span>
                <span className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-accent-bg ring-1 ring-accent/25">
                  <Icon className="size-5 text-accent-soft" />
                </span>
                <h3 className="text-[15px] font-semibold tracking-tight">{s.title}</h3>
                <p className="mt-2 text-[13px] leading-relaxed text-muted">{s.body}</p>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}

/* ------------------------------------------------------------- Features */
function Features() {
  const features = [
    { icon: Network, title: "Multi-agent graphs", body: "Visualize how a crew coordinates — dependencies, hand-offs, and live status for every agent." },
    { icon: Terminal, title: "World-class log streaming", body: "Realtime, searchable, filterable logs with reasoning traces, tool calls, and browser events on one timeline." },
    { icon: Globe, title: "Browser session replay", body: "Every navigation, click, and screenshot captured. Scrub the timeline and see exactly what the agent saw." },
    { icon: Gauge, title: "Cost & runtime tracking", body: "Per-run, per-agent token and dollar accounting with budgets and alerts before you overspend." },
    { icon: ShieldCheck, title: "Isolated sandboxes", body: "Each run executes in its own ephemeral, hardened sandbox. SOC 2 Type II, region-pinned." },
    { icon: Bell, title: "Notifications", body: "Completed, failed, browser errors, cost thresholds — delivered to Slack, email, or webhook." },
  ];
  return (
    <section id="features" className="mx-auto max-w-[1180px] px-6 py-24">
      <div className="mb-14 max-w-[640px]">
        <span className="text-[12px] font-semibold uppercase tracking-wider text-accent-soft">
          The control plane
        </span>
        <h2 className="mt-3 text-balance text-[32px] font-semibold tracking-tight md:text-[38px]">
          Everything you need to run agents in production.
        </h2>
        <p className="mt-4 text-[15px] leading-relaxed text-muted">
          One console for launching, observing, and debugging autonomous work —
          built for the density of real operations and the polish of a tool you
          live in all day.
        </p>
      </div>
      <div className="grid gap-px overflow-hidden rounded-xl border border-border bg-border md:grid-cols-3">
        {features.map((f) => {
          const Icon = f.icon;
          return (
            <div key={f.title} className="group bg-surface p-6 transition-colors hover:bg-surface-2">
              <span className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-surface-2 ring-1 ring-border transition-colors group-hover:bg-accent-bg group-hover:ring-accent/25">
                <Icon className="size-5 text-muted transition-colors group-hover:text-accent-soft" />
              </span>
              <h3 className="text-[15px] font-semibold tracking-tight">{f.title}</h3>
              <p className="mt-2 text-[13px] leading-relaxed text-muted">{f.body}</p>
            </div>
          );
        })}
      </div>
    </section>
  );
}

/* -------------------------------------------------------------- Metrics */
function Metrics() {
  const stats = [
    { v: "99.98%", l: "execution uptime" },
    { v: "2.1M+", l: "agent-hours run" },
    { v: "180ms", l: "log stream latency" },
    { v: "14", l: "regions worldwide" },
  ];
  return (
    <section className="border-y border-border/50 bg-surface/20 py-16">
      <div className="mx-auto grid max-w-[1180px] grid-cols-2 gap-8 px-6 md:grid-cols-4">
        {stats.map((s) => (
          <div key={s.l} className="text-center">
            <div className="text-[34px] font-semibold tracking-tight text-gradient">{s.v}</div>
            <div className="mt-1 text-[13px] text-muted">{s.l}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

/* ------------------------------------------------------------------ CTA */
function CTA() {
  return (
    <section className="mx-auto max-w-[1180px] px-6 py-28">
      <div className="relative overflow-hidden rounded-2xl border border-border-strong bg-surface px-8 py-16 text-center shadow-pop">
        <div className="bg-dots pointer-events-none absolute inset-0 opacity-40 [mask-image:radial-gradient(ellipse_50%_60%_at_50%_50%,black,transparent)]" />
        <div className="pointer-events-none absolute -top-24 left-1/2 h-48 w-[480px] -translate-x-1/2 rounded-full bg-accent/25 blur-3xl" />
        <div className="relative">
          <h2 className="mx-auto max-w-[560px] text-balance text-[34px] font-semibold leading-tight tracking-tight md:text-[42px]">
            Give your agents a place to live.
          </h2>
          <p className="mx-auto mt-4 max-w-[480px] text-[15px] text-muted">
            Deploy your first autonomous workflow in minutes. Watch it run long
            after you&apos;ve logged off.
          </p>
          <div className="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row">
            <Link
              href="/dashboard"
              className="group flex h-11 items-center gap-2 rounded-lg bg-accent px-6 text-[15px] font-medium text-accent-fg transition-colors hover:bg-accent-soft glow-accent"
            >
              Open the console
              <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
            </Link>
            <Link
              href="#"
              className="flex h-11 items-center rounded-lg border border-border bg-surface/60 px-6 text-[15px] font-medium text-fg transition-colors hover:border-border-strong"
            >
              Talk to sales
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}

/* --------------------------------------------------------------- Footer */
function Footer() {
  const cols: { h: string; links: string[] }[] = [
    { h: "Product", links: ["Workflows", "Agents", "Browser sessions", "Pricing"] },
    { h: "Developers", links: ["Documentation", "API reference", "CLI", "Status"] },
    { h: "Company", links: ["About", "Careers", "Blog", "Security"] },
  ];
  return (
    <footer className="border-t border-border bg-surface/20">
      <div className="mx-auto grid max-w-[1180px] gap-10 px-6 py-14 md:grid-cols-[1.4fr_1fr_1fr_1fr]">
        <div>
          <div className="flex items-center gap-2">
            <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-accent to-accent-soft">
              <Star className="h-4 w-4 text-white" />
            </span>
            <span className="text-[15px] font-semibold tracking-tight">Agently</span>
          </div>
          <p className="mt-4 max-w-[260px] text-[13px] leading-relaxed text-faint">
            The execution platform for long-running, autonomous AI agents.
          </p>
        </div>
        {cols.map((c) => (
          <div key={c.h}>
            <h4 className="mb-3 text-[12px] font-semibold uppercase tracking-wider text-faint">{c.h}</h4>
            <ul className="space-y-2.5">
              {c.links.map((l) => (
                <li key={l}>
                  <a href="#" className="text-[13px] text-muted transition-colors hover:text-fg">{l}</a>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      <div className="border-t border-border">
        <div className="mx-auto flex max-w-[1180px] flex-col items-center justify-between gap-3 px-6 py-5 text-[12px] text-faint sm:flex-row">
          <span>© 2026 Agently, Inc. All rights reserved.</span>
          <div className="flex items-center gap-5">
            <a href="#" className="transition-colors hover:text-muted">Privacy</a>
            <a href="#" className="transition-colors hover:text-muted">Terms</a>
            <a href="#" className="transition-colors hover:text-muted">SOC 2</a>
          </div>
        </div>
      </div>
    </footer>
  );
}

function Star({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className}>
      <path
        d="M12 3l2.4 5.1L20 9.3l-4 4 1 5.7-5-2.8-5 2.8 1-5.7-4-4 5.6-1.2L12 3z"
        fill="currentColor"
        opacity="0.92"
      />
    </svg>
  );
}
