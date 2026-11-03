import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Built files are embedded in the Flagfall binary and served from
// /theme/starter/. During `pnpm dev`, API calls go to a local server.
// If you copy this theme, change "starter" here and in theme.json.
export default defineConfig({
  base: "/theme/starter/",
  plugins: [svelte()],
  build: { outDir: "dist", emptyOutDir: true, assetsInlineLimit: 0, sourcemap: false },
  server: {
    proxy: {
      "/api": { target: "http://localhost:8080" },
      "/files": { target: "http://localhost:8080" },
      "/theme-assets": { target: "http://localhost:8080" },
    },
  },
});
