import { fileURLToPath, URL } from "node:url";

import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

const developmentPublicHost = process.env.KODEX_DEV_PUBLIC_HOST;
const developmentApiTarget = process.env.KODEX_DEV_API_TARGET;

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    target: "es2022",
    sourcemap: true,
  },
  server:
    developmentPublicHost && developmentApiTarget
      ? {
          allowedHosts: [developmentPublicHost],
          hmr: {
            clientPort: 443,
            host: developmentPublicHost,
            protocol: "wss",
          },
          watch: {
            interval: 500,
            usePolling: true,
          },
          proxy: {
            "/api": {
              changeOrigin: false,
              secure: false,
              target: developmentApiTarget,
              ws: true,
            },
          },
        }
      : undefined,
  test: {
    clearMocks: true,
    restoreMocks: true,
    environment: "node",
    include: ["src/**/*.test.ts", "e2e/storage-state.test.ts"],
  },
});
