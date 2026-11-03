import { defineConfig, loadEnv } from "vite";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "ORBIT_");
  const proxy = {
    "/api": {
      target: env.ORBIT_CORE_TARGET || "http://127.0.0.1:7620",
      changeOrigin: false,
    },
  };
  return {
    resolve: {
      alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
    },
    plugins: [
      react(),
      tailwindcss(),
      {
        name: "precompress-assets",
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
        },
      },
    ],
    server: {
      port: 5173,
      strictPort: true,
      proxy,
    },
    preview: { port: 5173, strictPort: true, proxy },
    build: { manifest: true, outDir: "dist", emptyOutDir: true },
  };
});
