# Node catalog

The catalog is shared by the Go planner, Python reasoner, and web builder.
Hand-written definitions live under `catalog/`; generated Activepieces metadata
lives under `pieces/`.

The planner uses a strict map/reduce flow: Voyage narrows the piece directory,
small Anthropic calls select node ids, and the larger `PLANNER_MODEL` call writes
the graph. The 15 built-ins are always supplied to reduce as the core vocabulary.

At runtime, catalog definitions execute through their declared real runtime:
HTTP, Browserbase, sandbox code, LLM, or piece worker. Missing credentials,
disabled tools, malformed templates, HTTP failures, and unsupported nodes fail
with a reason. No node records intent in place of execution.

Generated web files must be produced by `npm run build:web` and should not be
hand-edited.
