import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createServer } from "vite";
import { expect, it } from "vitest";

it("загружает схему и validator из дерева PWA без внешнего contracts", async () => {
  const root = mkdtempSync(join(tmpdir(), "kodex-pwa-schema-"));
  const modulePath =
    "src/features/managed-configurations/integration-package.ts";
  const generatedPath = "src/shared/api/generated/integration-package";
  const frontendRoot = new URL("../../../", import.meta.url);
  const canonicalSchema: unknown = JSON.parse(
    readFileSync(
      new URL(
        "../../../contracts/integrations/v1/integration-package.schema.json",
        frontendRoot,
      ),
      "utf8",
    ),
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
