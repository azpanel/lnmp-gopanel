import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { readFileSync } from "node:fs";
export default defineConfig(({ command, mode }) => ({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  build: {
    manifest: true,
    rollupOptions: {
      output: {
        // Do not move shared dependencies into a lazy chart chunk and thereby
        // turn it into a static dependency of the login entry.
        onlyExplicitManualChunks: true,
        manualChunks(id) {
          if (
            /\/node_modules\/(react|react-dom|react-router|react-router-dom|scheduler)\//.test(
              id,
            )
          )
            return "react";
          if (id.includes("/node_modules/recharts/")) return "charts";
          if (id.includes("/node_modules/zxcvbn/")) return "password-strength";
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
