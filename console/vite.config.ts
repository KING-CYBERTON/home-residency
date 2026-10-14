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
        // One vendor chunk, long-cached across deploys. Splitting React and the libraries
        // that use it into separate chunks made them import each other in a cycle, and
        // the built console crashed on load ("reading 'createContext'").
        manualChunks(id) {
          if (id.includes("node_modules")) return "vendor";
        },
      },
    },
    chunkSizeWarningLimit: 1500,
  },
  server: {
    proxy: {
      "/api": { target: process.env.API_PROXY_TARGET ?? "http://localhost:8080", changeOrigin: true },
    },
  },
});
