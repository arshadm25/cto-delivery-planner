import type { NextConfig } from "next";

// The browser only ever calls /api/v1 on this origin. Next forwards it to
// the Go API on the private network, so no API host or credential is
// exposed to browser code.
const apiInternal = process.env.API_INTERNAL_URL ?? "http://localhost:8080";

// Content-Security-Policy (P5-06). The app loads nothing from other origins:
// no CDN scripts, fonts or images, and the browser only calls /api/v1 here.
// Next.js inlines small bootstrap scripts, so script-src needs
// 'unsafe-inline'; the policy still blocks every other origin, plugins,
// framing and <base> or form hijacking. `next dev` also needs eval and the
// hot-reload websocket.
const dev = process.env.NODE_ENV !== "production";
const contentSecurityPolicy = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${dev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "font-src 'self' data:",
  `connect-src 'self'${dev ? " ws:" : ""}`,
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  async rewrites() {
    return [{ source: "/api/v1/:path*", destination: `${apiInternal}/api/v1/:path*` }];
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Frame-Options", value: "DENY" },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "same-origin" },
          { key: "Content-Security-Policy", value: contentSecurityPolicy },
        ],
      },
    ];
  },
};

export default nextConfig;
