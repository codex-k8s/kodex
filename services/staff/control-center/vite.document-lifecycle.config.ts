import { fileURLToPath, URL } from "node:url";
import { mergeConfig } from "vite";
import base from "./vite.config";
export default mergeConfig(base, {
  resolve: {
    alias: [
      {
        find: "@/shared/config/runtime",
        replacement: fileURLToPath(
          new URL(
            "./e2e/fixtures/document-lifecycle-runtime.ts",
            import.meta.url,
          ),
        ),
      },
      {
        find: "@",
        replacement: fileURLToPath(new URL("./src", import.meta.url)),
      },
    ],
  },
  build: {
    outDir: "dist-document-lifecycle",
    rolldownOptions: {
      input: fileURLToPath(
        new URL("./e2e/fixtures/document-lifecycle.html", import.meta.url),
      ),
    },
  },
});
