# Web data contract

The Next.js app is a live client of the Go API. It does not ship a fixture
dataset and does not synthesize logs, browser screenshots, activity events, run
costs, or status counts.

Pages poll the API where live updates are needed. Logs are append-only and fetched
by sequence number. Running durations use the browser clock after mount; persisted
timestamps remain the source of truth. Costs shown in the UI are the values
reported by the API.

When the API is unavailable, pages show an empty/loading/error state and identify
the endpoint problem. They do not display a successful-looking demo workspace.
