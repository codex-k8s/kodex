import { fileURLToPath, URL } from "node:url";
import { mergeConfig } from "vite";
import base from "./vite.config";

export default mergeConfig(base, {
  build: {
    outDir: "dist-synthetic",
    rolldownOptions: {
      input: {
        application: fileURLToPath(new URL("./index.html", import.meta.url)),
        voice: fileURLToPath(
          new URL("./e2e/fixtures/voice.html", import.meta.url),
        ),
      },
    },
  },
});
