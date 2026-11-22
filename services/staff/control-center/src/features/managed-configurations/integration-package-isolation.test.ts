import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";
import { expect, it } from "vitest";

const frontendRoot = new URL("../../../", import.meta.url);
const schemaPath = "contracts/integrations/v1/integration-package.schema.json";
const generatedPath = "src/shared/api/generated/integration-package";
const canonicalSchemaUrl = new URL(`../../../${schemaPath}`, frontendRoot);

it("загружает схему и validator из дерева PWA без внешнего contracts", async () => {
  const root = mkdtempSync(join(tmpdir(), "kodex-pwa-schema-"));
  const modulePath =
    "src/features/managed-configurations/integration-package.ts";
  const canonicalSchema: unknown = JSON.parse(
    readFileSync(canonicalSchemaUrl, "utf8"),
  );
  let server: Awaited<ReturnType<typeof createServer>> | undefined;
  try {
    mkdirSync(dirname(join(root, modulePath)), { recursive: true });
    cpSync(new URL(modulePath, frontendRoot), join(root, modulePath));
    cpSync(new URL(generatedPath, frontendRoot), join(root, generatedPath), {
      recursive: true,
    });
    server = await createServer({
      configFile: false,
      root,
      logLevel: "silent",
      resolve: { alias: { "@": join(root, "src") } },
      server: {
        middlewareMode: true,
        watch: null,
        fs: { strict: true, allow: [root] },
      },
      optimizeDeps: { noDiscovery: true, entries: [] },
    });
    const module = (await server.ssrLoadModule(`/${modulePath}`)) as {
      packageSchema: unknown;
      packageDiagnostics: (value: unknown) => string[];
    };
    expect(module.packageSchema).toEqual(canonicalSchema);
    expect(module.packageDiagnostics({}).length).toBeGreaterThan(0);
  } finally {
    await server?.close();
    rmSync(root, { recursive: true, force: true });
  }
}, 10_000);

it("воспроизводит валидатор с симлинком на зависимости из разных рабочих каталогов", () => {
  const root = mkdtempSync(join(tmpdir(), "kodex-schema-codegen-"));
  const frontend = join(root, "services/staff/control-center");
  const generator = "tools/generate-integration-schema.mjs";
  try {
    mkdirSync(join(frontend, "tools"), { recursive: true });
    for (const tool of [generator, "tools/frontend-codegen-barrier.mjs"]) {
      cpSync(new URL(tool, frontendRoot), join(frontend, tool));
    }
    mkdirSync(dirname(join(root, schemaPath)), { recursive: true });
    cpSync(canonicalSchemaUrl, join(root, schemaPath));
    symlinkSync(
      fileURLToPath(new URL("node_modules", frontendRoot)),
      join(frontend, "node_modules"),
      "dir",
    );
    const validator = `${generatedPath}/validate.js`;
    const expected = readFileSync(new URL(validator, frontendRoot), "utf8");
    for (const cwd of [frontend, root]) {
      execFileSync(process.execPath, [join(frontend, generator)], {
        cwd,
        timeout: 5_000,
      });
      expect(readFileSync(join(frontend, validator), "utf8")).toBe(expected);
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}, 15_000);
