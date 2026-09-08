import { chmod, link, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, describe, expect, test, vi } from "vitest";
import {
  readAPISessionStorageState,
  readStorageState,
  type BrowserStorageState,
} from "./storage-state";

const origin = "https://control.kodex.test";
const directories: string[] = [];
function state(
  proxy: "none" | "single" | "chunks" = "chunks",
): BrowserStorageState {
  const base = {
    domain: "control.kodex.test",
    path: "/",
    expires: Math.floor(Date.now() / 1000) + 3600,
    secure: true,
    sameSite: "Strict" as const,
  };
  const cookies: BrowserStorageState["cookies"] = [
    {
      ...base,
      name: "__Host-kodex-session",
      value: `v1.${"a".repeat(43)}`,
      httpOnly: true,
    },
    {
      ...base,
      name: "__Host-kodex-csrf",
      value: "b".repeat(43),
      httpOnly: false,
    },
  ];
  const names =
    proxy === "none"
      ? []
      : proxy === "single"
        ? ["_kodex_control_center_oauth2"]
        : ["_kodex_control_center_oauth2_0", "_kodex_control_center_oauth2_1"];
  for (const name of names)
    cookies.push({
      ...base,
      name,
      value: "synthetic-proxy-cookie",
      httpOnly: true,
      sameSite: "Lax",
    });
  return { cookies, origins: [] };
}
async function fixture(value: unknown = state()): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "k1268-"));
  directories.push(directory);
  await chmod(directory, 0o700);
  const path = join(directory, "session.json");
  await writeFile(path, JSON.stringify(value), { mode: 0o600 });
  return path;
}
afterEach(async () => {
  vi.unstubAllEnvs();
  vi.resetModules();
  await Promise.all(
    directories
      .splice(0)
      .map((directory) => rm(directory, { recursive: true, force: true })),
  );
});

describe("ограниченный API session reader", () => {
  test.each(["none", "single", "chunks"] as const)(
    "принимает BFF и proxy profile %s без bootstrap ослабления",
    async (proxy) => {
      const input = state(proxy);
      const path = await fixture(input);
      expect(readAPISessionStorageState(path, origin)).toEqual(input);
      expect(() => readStorageState(path)).toThrow(
        "bootstrap storage state contains a Kodex API cookie",
      );
    },
  );
  test("публичный config принимает private API state с proxy chunks без браузера или сети", async () => {
    const path = await fixture();
    vi.stubEnv("KODEX_E2E_CHECK_ONLY", "0");
    vi.stubEnv("KODEX_E2E_BASE_URL", origin);
    vi.stubEnv("KODEX_E2E_STORAGE_STATE", path);
    vi.stubEnv(
      "KODEX_E2E_CONFIRM_DISPOSABLE",
      "I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION",
    );
    vi.stubEnv("KODEX_E2E_RESOURCE_PREFIX", "mvp1268-loader");
    vi.stubEnv("KODEX_E2E_PROFILE", "web-only");
    vi.resetModules();
    const { default: config } = await import("./session-renewal.config");
    const handoff = config.use?.storageState as BrowserStorageState;
    expect(handoff.cookies.map((cookie) => cookie.name)).toEqual(
      state().cookies.map((cookie) => cookie.name),
    );
    expect(handoff.origins).toEqual([]);
    expect(config.use?.trace).toBe("off");
    const { loadE2EEnvironment } = await import("./environment");
    expect(() => loadE2EEnvironment()).toThrow(
      "bootstrap storage state contains a Kodex API cookie",
    );
  });
  test.each([
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.slice(1),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: [...input.cookies, input.cookies[0]],
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) => ({
        ...cookie,
        domain: "foreign.invalid",
      })),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) => ({ ...cookie, secure: false })),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) => ({ ...cookie, expires: 1 })),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.filter((cookie) => !cookie.name.endsWith("_0")),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: [
        ...input.cookies,
        {
          ...input.cookies[0],
          name: "KEYCLOAK_SESSION",
          domain: "identity.invalid",
          value: "private-sentinel",
        },
      ],
    }),
    (input: BrowserStorageState) => ({
      ...input,
      origins: [
        {
          origin,
          localStorage: [{ name: "private", value: "private-sentinel" }],
        },
      ],
    }),
  ])(
    "закрыто отклоняет неполное, чужое или небезопасное browser material",
    async (change) => {
      const path = await fixture(change(state()));
      expect(() => readAPISessionStorageState(path, origin)).toThrow();
      try {
        readAPISessionStorageState(path, origin);
      } catch (error) {
        expect(String(error)).not.toContain("private-sentinel");
      }
    },
  );
  test("private descriptor guards отклоняют symlink, hardlink, mode и malformed JSON", async () => {
    const path = await fixture();
    const symbolic = `${path}.symlink`;
    await symlink(path, symbolic);
    expect(() => readAPISessionStorageState(symbolic, origin)).toThrow();
    const hard = `${path}.hardlink`;
    await link(path, hard);
    expect(() => readAPISessionStorageState(path, origin)).toThrow();
    await rm(hard);
    await chmod(path, 0o644);
    expect(() => readAPISessionStorageState(path, origin)).toThrow();
    await chmod(path, 0o600);
    await chmod(dirname(path), 0o755);
    expect(() => readAPISessionStorageState(path, origin)).toThrow();
    await chmod(dirname(path), 0o700);
    await writeFile(path, '{"private-sentinel":');
    expect(() => readAPISessionStorageState(path, origin)).toThrow(
      "storage JSON is invalid",
    );
  });
});
