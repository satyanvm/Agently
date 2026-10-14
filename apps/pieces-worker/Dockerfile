# Build context: repository root.
#   docker build -f apps/pieces-worker/Dockerfile .
FROM node:22-slim AS build
WORKDIR /app/apps/pieces-worker
COPY apps/pieces-worker/package.json apps/pieces-worker/package-lock.json ./
RUN npm ci
COPY apps/pieces-worker/tsconfig.json ./
COPY apps/pieces-worker/src ./src
RUN npm run build && npm prune --omit=dev

FROM node:22-slim
ENV NODE_ENV=production PIECES_HTTP_PORT=7391
WORKDIR /app/apps/pieces-worker
COPY --from=build /app/apps/pieces-worker/node_modules ./node_modules
COPY --from=build /app/apps/pieces-worker/dist ./dist
COPY apps/pieces-worker/package.json ./
USER node
EXPOSE 7391
CMD ["node", "dist/worker.js"]
