import type { NextConfig } from "next";

// The backend is the standalone Go API (apps/api), served on :8090 (:8080 is the
// Temporal UI from docker-compose.yml). In development we proxy /api/* to it so the
// frontend keeps calling same-origin /api paths with no CORS and no URL changes.
// Override the target with API_PROXY_TARGET when the API runs elsewhere.
const API_TARGET = process.env.API_PROXY_TARGET ?? "http://localhost:8090";

const nextConfig: NextConfig = {
  experimental: {
    // Compiling a prompt is several LLM round trips (route, map fan-out,
    // reduce) and takes ~4-5 min on a local 8B model (Ollama). The dev
    // proxy's default (~30s) hung up mid-compile and surfaced as a 500.
    proxyTimeout: 15 * 60 * 1000,
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${API_TARGET}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
