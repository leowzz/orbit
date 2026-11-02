import { defineConfig } from "vite";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "keep-embed-directory",
      closeBundle() {
        const assets = new URL("./dist/assets/", import.meta.url);
        for (const name of readdirSync(assets)) {
          if (!/\.(js|css|svg)$/.test(name)) continue;
          const source = readFileSync(new URL(name, assets));
          writeFileSync(
            new URL(name + ".gz", assets),
            gzipSync(source, { level: 9 }),
          );
        }
        writeFileSync(new URL("./dist/.gitkeep", import.meta.url), "");
      },
    },
  ],
  server: {
    port: 5173,
    strictPort: true,
    proxy: { "/api": { target: "http://127.0.0.1:7620", changeOrigin: false } },
  },
  build: { manifest: true, outDir: "dist", emptyOutDir: true },
});
