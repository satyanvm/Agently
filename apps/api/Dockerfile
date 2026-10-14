# Build context: repository root.
#   docker build -f apps/api/Dockerfile .
FROM golang:1.25 AS build
WORKDIR /src/apps/api
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
# The API resolves its data files by walking up from the working directory to
# the repo layout (packages/nodes/..., apps/web/components/builder/...).
WORKDIR /app
COPY packages/nodes/catalog ./packages/nodes/catalog
# index.json plus the embeddings sidecar (npm run gen:embeddings) — the API
# refuses to start without a sidecar matching PLANNER_EMBED_MODEL.
COPY packages/nodes/pieces ./packages/nodes/pieces
COPY apps/web/components/builder/credential-types.generated.json ./apps/web/components/builder/
COPY --from=build /out/api /app/api
ENV ENV=production API_ADDR=:8090
EXPOSE 8090
ENTRYPOINT ["/app/api"]
