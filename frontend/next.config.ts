import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Keep `next dev` and `next build` from overwriting one another's chunks.
  distDir: process.env.NODE_ENV === "development" ? ".next-dev" : ".next",
  async rewrites() {
    return [{
      source: "/api/:path*",
      destination: `${process.env.API_ORIGIN ?? "http://localhost:8080"}/api/:path*`,
    }];
  },
};

export default nextConfig;
