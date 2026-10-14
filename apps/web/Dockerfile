# Build context: repository root.
#   docker build -f apps/web/Dockerfile --build-arg API_PROXY_TARGET=http://api:8090 .
FROM node:22-slim AS build
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY packages/contracts ./packages/contracts
COPY apps/web ./apps/web
RUN pnpm install --frozen-lockfile
# Next.js bakes rewrite destinations in at build time.
ARG API_PROXY_TARGET=http://api:8090
ENV API_PROXY_TARGET=$API_PROXY_TARGET NEXT_TELEMETRY_DISABLED=1
RUN pnpm --filter @agently/web build

FROM node:22-slim
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1
WORKDIR /app
COPY --from=build /app /app
USER node
EXPOSE 3000
WORKDIR /app/apps/web
CMD ["node_modules/.bin/next", "start", "-p", "3000"]
