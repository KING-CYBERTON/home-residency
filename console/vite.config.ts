import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The console is served from the same origin as the API in every
// environment (CloudFront routes /api/* to the Go service), so the dev
// server proxies /api to a local backend instead of relying on CORS.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  build: {
    rollupOptions: {
      output: {
        // Long-cached vendor chunks change rarely; app code changes every deploy.
        manualChunks: {
          react: ["react", "react-dom", "react-router"],
          refine: ["@refinedev/core", "@refinedev/react-router", "@tanstack/react-query"],
          ui: ["radix-ui", "lucide-react", "sonner"],
        },
      },
    },
  },
  server: {
    proxy: {
      "/api": { target: process.env.API_PROXY_TARGET ?? "http://localhost:8080", changeOrigin: true },
    },
  },
});
