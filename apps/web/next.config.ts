import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Workspace packages are plain TS consumed directly from source; let Next
  // transpile them (route handlers import @agently/core/platform + contracts).
  transpilePackages: ["@agently/core", "@agently/contracts"],
  webpack: (config) => {
    // The workspace packages use NodeNext-style ".js" import specifiers that
    // actually resolve to ".ts" source. Teach webpack to try ".ts" first.
    config.resolve.extensionAlias = {
      ".js": [".ts", ".tsx", ".js", ".jsx"],
      ".mjs": [".mts", ".mjs"],
      ".cjs": [".cts", ".cjs"],
    };
    return config;
  },
};

export default nextConfig;
