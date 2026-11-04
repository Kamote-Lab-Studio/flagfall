import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Built files are embedded in the Flagfall binary and served from
// /theme/neon/. During `pnpm dev`, API calls go to a local server.
export default defineConfig({
  base: "/theme/neon/",
  plugins: [svelte()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsInlineLimit: 0,
    sourcemap: false,
    rollupOptions: {
      // admin.css restyles the admin panel; theme.json's adminStyle points at
      // it, so it keeps a fixed name (served with no-cache).
      input: { index: "index.html", admin: "src/admin.css" },
      output: {
        assetFileNames: (info) =>
          info.names?.includes("admin.css") ? "admin.css" : "assets/[name]-[hash][extname]",
      },
    },
  },
  server: {
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/files": { target: "http://localhost:8080" },
    },
  },
});
