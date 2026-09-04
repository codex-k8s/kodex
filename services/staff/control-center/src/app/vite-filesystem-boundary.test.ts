import { fileURLToPath, URL } from "node:url";

import { isFileLoadingAllowed, normalizePath, resolveConfig } from "vite";
import { beforeAll, describe, expect, it } from "vitest";

import viteConfig, {
  controlCenterFileSystemBoundary,
  controlCenterReloadPollIntervalMs,
  withoutViteHMRConnection,
  withoutViteHMRClient,
} from "../../vite.config";

const controlCenterRoot = fileURLToPath(new URL("../..", import.meta.url));
const repositoryRoot = fileURLToPath(
  new URL("../../../../../", import.meta.url),
);
const asFileSystemPath = (path: string) => normalizePath(path);

describe("filesystem boundary Vite dev-server", () => {
  let resolvedConfig: Awaited<ReturnType<typeof resolveConfig>>;

  beforeAll(async () => {
    resolvedConfig = await resolveConfig(
      {
        ...viteConfig,
        configFile: false,
        logLevel: "silent",
        root: controlCenterRoot,
      },
      "serve",
    );
  });

  it("разрешает только каталог Control Center, необходимый для HMR", () => {
    expect(controlCenterFileSystemBoundary).toMatchObject({
      strict: true,
      allow: [controlCenterRoot],
    });
    expect(
      isFileLoadingAllowed(
        resolvedConfig,
        asFileSystemPath(`${controlCenterRoot}/src/main.ts`),
      ),
    ).toBe(true);
    expect(
      isFileLoadingAllowed(
        resolvedConfig,
        asFileSystemPath(`${repositoryRoot}/AGENTS.md`),
      ),
    ).toBe(false);
  });

  it("проверяет remote revision с bounded интервалом", () => {
    expect(controlCenterReloadPollIntervalMs).toBe(1_000);
  });

  it("заменяет встроенный Vite HMR client на remote polling reload", () => {
    expect(
      withoutViteHMRClient(`<!doctype html>
<html><head><script type="module" src="/@vite/client"></script></head></html>`),
    ).toBe("<!doctype html>\n<html><head></head></html>");
  });

  it("сохраняет Vite client helpers без подключения HMR transport", () => {
    const source = `const transport = {};
transport.connect(createHMRHandler(handleMessage));
export { transport };`;
    expect(withoutViteHMRConnection(source)).toBe(
      "const transport = {};\nexport { transport };",
    );
    expect(() => withoutViteHMRConnection("export const value = 1;")).toThrow(
      "Vite client HMR bootstrap is missing",
    );
  });

  it.each([
    ".env",
    ".env.local",
    ".kodex-env",
    ".kodex-remote-env",
    ".kodex-remote-env.example",
    ".npmrc",
    ".netrc",
    "identity.key",
    "identity.der",
    "identity.pem",
    "identity.p12",
    "access.token",
    "runtime.secret",
    "credentials.private",
    "id_ed25519",
    "credentials",
    "credentials.json",
    "token.yaml",
    ".aws/credentials",
    ".config/gh/hosts.yml",
    ".docker/config.json",
    ".kube/config",
  ])("закрыто отклоняет private material %s внутри allow", (fileName) => {
    expect(
      isFileLoadingAllowed(
        resolvedConfig,
        asFileSystemPath(`${controlCenterRoot}/${fileName}`),
      ),
    ).toBe(false);
  });
});
