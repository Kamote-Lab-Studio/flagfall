import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Built files are embedded in the Flagfall binary and served from /admin/.
// During `pnpm dev`, API calls go to a local server.
export default defineConfig({
  base: "/admin/",
  plugins: [svelte()],
  build: { outDir: "dist", emptyOutDir: true, assetsInlineLimit: 0, sourcemap: false },
  server: {
    proxy: {
      "/admin/api": { target: "http://localhost:8080" },
      "/api": { target: "http://localhost:8080" },
      "/files": { target: "http://localhost:8080" },
    },
  },
});
