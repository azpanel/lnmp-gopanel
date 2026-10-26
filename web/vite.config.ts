import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { readFileSync } from "node:fs";
export default defineConfig(({ command, mode }) => ({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          charts: ["recharts"],
          react: ["react", "react-dom", "react-router-dom"],
        },
      },
    },
  },
  server: {
    https:
      command === "serve" && mode !== "test"
        ? {
            cert: readFileSync(
              path.resolve(import.meta.dirname, "../tls/panel.crt"),
            ),
            key: readFileSync(
              path.resolve(import.meta.dirname, "../tls/panel.key"),
            ),
          }
        : undefined,
    proxy: {
      "/api": {
        target: "https://127.0.0.1:8090",
        secure: false, // Trust the local development backend's self-signed certificate.
        changeOrigin: false,
      },
    },
  },
}));
