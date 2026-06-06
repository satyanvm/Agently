import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // packages/core is plain TS consumed directly from source; let Next transpile it.
  transpilePackages: ["@agently/core"],
};

export default nextConfig;
