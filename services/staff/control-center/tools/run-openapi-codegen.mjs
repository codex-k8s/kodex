import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { withFrontendCodegen } from "./frontend-codegen-barrier.mjs";

if (process.argv.length !== 2)
  throw new Error("OpenAPI generator arguments are not supported");
await withFrontendCodegen("openapi", () => {
  const result = spawnSync(
    process.execPath,
    [
      fileURLToPath(
        new URL(
          "../node_modules/@hey-api/openapi-ts/bin/run.js",
          import.meta.url,
        ),
      ),
      "-f",
      "openapi-ts.config.mjs",
    ],
    {
      cwd: fileURLToPath(new URL("..", import.meta.url)),
      stdio: "inherit",
      timeout: 120_000,
    },
  );
  if (result.error || result.status !== 0)
    throw new Error("Frontend OpenAPI code generation failed");
});
