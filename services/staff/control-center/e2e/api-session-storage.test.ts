import {
  chmod,
  link,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, describe, expect, test, vi } from "vitest";
import {
  readAPISessionStorageState,
  writeAuthenticatedStorageState,
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

describe("API-only writer и строгий reader", () => {
  test.each(["none", "single", "chunks"] as const)(
    "атомарно сохраняет точный transport набор %s без IdP, OAuth CSRF и origins",
    async (proxy) => {
      const selected = state(proxy);
      const first = selected.cookies[0];
      if (!first) throw new Error("Missing fixture cookie");
      const foreign = [
        {
          ...first,
          name: "KEYCLOAK_SESSION",
          domain: "identity.kodex.test",
          value: "synthetic-idp-session",
        },
        {
          ...first,
          name: "KEYCLOAK_IDENTITY",
          domain: "identity.kodex.test",
          value: "synthetic-idp-identity",
        },
        {
          ...first,
          name: "AUTH_SESSION_ID",
          domain: "identity.kodex.test",
          value: "synthetic-idp-auth",
        },
        { ...first, name: "KEYCLOAK_SESSION", value: "synthetic-control-idp" },
        {
          ...first,
          name: "_kodex_control_center_oauth2_csrf",
          value: "synthetic-oauth-csrf",
        },
        {
          ...first,
          name: "_kodex_control_center_oauth2_csrf_attempt",
          value: "synthetic-oauth-attempt",
        },
        {
          ...first,
          name: "_kodex_control_center_oauth2_attempt_csrf",
          value: "synthetic-oauth-csrf-suffix",
        },
        {
          ...first,
          name: "unrelated",
          domain: "foreign.invalid",
          expires: 1,
          value: "synthetic-foreign",
        },
      ];
      const input = {
        cookies: [...foreign, ...selected.cookies].reverse(),
        origins: [
          {
            origin,
            localStorage: [{ name: "draft", value: "synthetic-private-draft" }],
          },
          {
            origin: "https://identity.kodex.test",
            localStorage: [{ name: "idp", value: "synthetic-idp-storage" }],
          },
        ],
      };
      const unchanged = structuredClone(input);
      const path = await fixture();
      await writeAuthenticatedStorageState(path, input, origin);
      expect(input).toEqual(unchanged);
      expect(readAPISessionStorageState(path, origin)).toEqual(selected);
      expect((await stat(path)).mode & 0o777).toBe(0o600);
      expect(await readdir(dirname(path))).toEqual(["session.json"]);
      const persisted = await readFile(path, "utf8");
      for (const cookie of foreign)
        expect(persisted).not.toContain(cookie.value);
      expect(persisted).not.toContain("synthetic-private-draft");
      expect(persisted).not.toContain("synthetic-idp-storage");
      expect(() => readStorageState(path)).toThrow("Kodex API cookie");
      const dirtyPath = await fixture(input);
      expect(() => readAPISessionStorageState(dirtyPath, origin)).toThrow(
        "foreign browser material",
      );
    },
  );
  test("сохраняет разрешённые BFF session cookies без искусственного expiry", async () => {
    const input = state("none");
    const sessionCookies = {
      ...input,
      cookies: input.cookies.map((cookie) => ({ ...cookie, expires: -1 })),
    };
    const path = await fixture();
    await writeAuthenticatedStorageState(path, sessionCookies, origin);
    expect(readAPISessionStorageState(path, origin)).toEqual(sessionCookies);
  });
  test.each([
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) =>
        cookie.name.startsWith("_kodex") ? { ...cookie, expires: 1 } : cookie,
      ),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: [...input.cookies, input.cookies[0]],
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: [
        ...input.cookies,
        { ...input.cookies[0], domain: "foreign.invalid" },
      ],
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.slice(1),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) => ({ ...cookie, expires: 1 })),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) =>
        cookie.name === "__Host-kodex-csrf"
          ? { ...cookie, httpOnly: true }
          : cookie,
      ),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) => ({
        ...cookie,
        domain: ".control.kodex.test",
      })),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) => ({
        ...cookie,
        partitionKey: origin,
      })),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.filter((cookie) => !cookie.name.endsWith("_0")),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: [
        ...input.cookies,
        { ...input.cookies[2], name: "_kodex_control_center_oauth2" },
      ],
    }),
    (input: BrowserStorageState) => ({
      ...input,
      cookies: input.cookies.map((cookie) =>
        cookie.name.startsWith("_kodex")
          ? { ...cookie, expires: Math.floor(Date.now() / 1000) + 9 * 3600 }
          : cookie,
      ),
    }),
    (input: BrowserStorageState) => ({
      ...input,
      origins: [
        { origin: "https://identity.kodex.test/private", localStorage: [] },
      ],
    }),
  ])(
    "отклоняет неоднозначность/границы/expiry до замены прежнего файла",
    async (change) => {
      const path = await fixture();
      const previous = await readFile(path, "utf8");
      await expect(
        writeAuthenticatedStorageState(path, change(state()), origin),
      ).rejects.toThrow();
      expect(await readFile(path, "utf8")).toBe(previous);
      expect(await readdir(dirname(path))).toEqual(["session.json"]);
      expect(readAPISessionStorageState(path, origin)).toEqual(
        JSON.parse(previous),
      );
    },
  );
  test.each([
    "http://control.kodex.test",
    "https://control.kodex.test/",
    "https://control.kodex.test?token=fixture",
  ])("не нормализует неточный expected origin %s", async (invalidOrigin) => {
    const path = await fixture();
    const before = await readFile(path, "utf8");
    await expect(
      writeAuthenticatedStorageState(path, state(), invalidOrigin),
    ).rejects.toThrow();
    expect(await readFile(path, "utf8")).toBe(before);
  });
});
