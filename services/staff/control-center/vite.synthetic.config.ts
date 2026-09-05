import { fileURLToPath, URL } from "node:url";
import { mergeConfig } from "vite";
import base from "./vite.config";

export default mergeConfig(base, {
  build: {
    outDir: "dist-synthetic",
    rolldownOptions: {
      input: {
        sttCatalog: fileURLToPath(
          new URL("./e2e/fixtures/stt-catalog.html", import.meta.url),
        ),
        mailbox: fileURLToPath(
          new URL("./e2e/fixtures/mailbox.html", import.meta.url),
        ),
        runtimeDetail: fileURLToPath(
          new URL("./e2e/fixtures/runtime-detail.html", import.meta.url),
        ),
        checkpoint: fileURLToPath(
          new URL("./e2e/fixtures/checkpoint.html", import.meta.url),
        ),
        application: fileURLToPath(new URL("./index.html", import.meta.url)),
        voice: fileURLToPath(
          new URL("./e2e/fixtures/voice.html", import.meta.url),
        ),
        models: fileURLToPath(
          new URL("./e2e/fixtures/models.html", import.meta.url),
        ),
        impact: fileURLToPath(
          new URL("./e2e/fixtures/impact.html", import.meta.url),
        ),
      },
    },
  },
});
