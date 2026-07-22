import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "/control-center/",
  plugins: [vue()],
  test: {
    environment: "jsdom",
  },
});
