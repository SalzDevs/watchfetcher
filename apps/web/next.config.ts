import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  turbopack: {
    root: __dirname,
  },
  images: {
    remotePatterns: [
      { hostname: "**.chrono24.com" },
      { hostname: "**.chrono24.*.com" },
      { hostname: "**.bobswatches.com" },
      { hostname: "**.the1916company.com" },
      { hostname: "**.watchfinder.co.uk" },
      { hostname: "**.watchfinder.*.co.uk" },
      { hostname: "cdn*.chrono24.com" },
      { hostname: "images.watchfinder.co.uk" },
      { hostname: "cdn11.bigcommerce.com" },
      { hostname: "**.bigcommerce.com" },
    ],
  },
};

export default nextConfig;
