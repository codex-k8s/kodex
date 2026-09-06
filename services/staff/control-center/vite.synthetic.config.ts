import { fileURLToPath, URL } from "node:url";
import { mergeConfig } from "vite";
import base from "./vite.config";

export default mergeConfig(base, {
  build: {
    outDir: "dist-synthetic",
    rolldownOptions: {
      input: {
        sttActivation: fileURLToPath(
          new URL("./e2e/fixtures/stt-activation.html", import.meta.url),
        ),
        cards: fileURLToPath(
          new URL("./e2e/fixtures/cards.html", import.meta.url),
        ),
        resourceSelection: fileURLToPath(
          new URL("./e2e/fixtures/resource-selection.html", import.meta.url),
        ),
        automationPreview: fileURLToPath(
          new URL("./e2e/fixtures/automation-preview.html", import.meta.url),
        ),
        gateNavigation: fileURLToPath(
          new URL("./e2e/fixtures/gate-navigation.html", import.meta.url),
        ),
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
