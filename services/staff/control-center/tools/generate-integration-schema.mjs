import { readFile, writeFile, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import Ajv2020 from "ajv/dist/2020.js";
import standaloneCode from "ajv/dist/standalone/index.js";
import { build } from "esbuild";

const schemaUrl = new URL(
  "../../../../contracts/integrations/v1/integration-package.schema.json",
  import.meta.url,
);
const output = new URL(
  "../src/shared/api/generated/integration-package/",
  import.meta.url,
);
const schema = JSON.parse(await readFile(schemaUrl, "utf8"));
const ajv = new Ajv2020({
  allErrors: true,
  strictTypes: true,
  strictRequired: false,
  logger: {
    log: console.log,
    error: console.error,
    warn: (...messages) => {
      throw new Error(`Schema compiler warning: ${messages.join(" ")}`);
    },
  },
  code: { source: true, esm: true },
});
const validate = ajv.compile(schema);
const result = await build({
  stdin: {
    contents: standaloneCode(ajv, validate),
    resolveDir: fileURLToPath(new URL("../", import.meta.url)),
    sourcefile: "integration-package.js",
  },
  bundle: true,
  platform: "browser",
  format: "esm",
  write: false,
});
if (result.warnings.length > 0) {
  throw new Error("Generated schema bundle contains compiler warnings");
}
await mkdir(output, { recursive: true });
await writeFile(
  new URL("validate.js", output),
  "// Сгенерировано tools/generate-integration-schema.mjs. Не редактировать.\n" +
    result.outputFiles[0].text,
);
await writeFile(
  new URL("validate.d.ts", output),
  'import type { ValidateFunction } from "ajv";\ndeclare const validate: ValidateFunction;\nexport default validate;\n',
);
