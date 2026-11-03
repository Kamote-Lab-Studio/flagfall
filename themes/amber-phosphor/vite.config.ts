import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Built files are embedded in the Flagfall binary and served from
// /theme/amber-phosphor/. During `pnpm dev`, API calls go to a local server.
export default defineConfig({
  base: "/theme/amber-phosphor/",
  plugins: [svelte()],
  build: { outDir: "dist", emptyOutDir: true, assetsInlineLimit: 0, sourcemap: false },
  server: {
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/files": { target: "http://localhost:8080" },
    },
  },
});
