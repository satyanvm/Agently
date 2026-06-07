import type { NextConfig } from "next";

// The backend is the standalone Go API (apps/api), served on :8080. In
// development we proxy /api/* to it so the frontend keeps calling same-origin
// /api paths with no CORS and no URL changes. Override the target with
// API_PROXY_TARGET when the API runs elsewhere.
const API_TARGET = process.env.API_PROXY_TARGET ?? "http://localhost:8080";

const nextConfig: NextConfig = {
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
