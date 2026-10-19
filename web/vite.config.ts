import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const backend = process.env.PLEXROUTER_BACKEND ?? "http://localhost:8080";

export default defineConfig({
  base: "/",
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": { target: backend, changeOrigin: true },
      "/v1": { target: backend, changeOrigin: true },
    },
  },
});
