import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "vitest";

import {
  readStorageState,
  withoutKodexAPICookies,
  writeStorageState,
} from "./storage-state";

const directories: string[] = [];

afterEach(async () => {
  await Promise.all(
    directories
      .splice(0)
      .map((directory) => rm(directory, { recursive: true })),
  );
});

describe("E2E storage state", () => {
  test("атомарно записывает только SSO state с mode 0600", async () => {
    const directory = await protectedDirectory();
    const path = join(directory, "owner.json");
    const state = withoutKodexAPICookies({
      cookies: [
        cookie("KEYCLOAK_SESSION", "sso"),
        cookie("__Host-kodex-session", "api"),
        cookie("__Host-kodex-csrf", "csrf"),
      ],
      origins: [],
    });

    await writeStorageState(path, state);
    expect((await stat(path)).mode & 0o777).toBe(0o600);
    expect(readStorageState(path)).toEqual({
      cookies: [cookie("KEYCLOAK_SESSION", "sso")],
      origins: [],
    });
    expect(
      (await readdir(directory)).filter((name) => name.endsWith(".tmp")),
    ).toEqual([]);
  });

  test("закрыто отклоняет symlink при чтении и записи", async () => {
    const directory = await protectedDirectory();
    const victim = join(directory, "victim.json");
    const link = join(directory, "owner.json");
    await writeFile(victim, "unchanged", { mode: 0o600 });
    await symlink(victim, link);

    expect(() => readStorageState(link)).toThrow();
    await expect(
      writeStorageState(link, { cookies: [], origins: [] }),
    ).rejects.toThrow("unsafe");
    expect(await readFile(victim, "utf8")).toBe("unchanged");
  });

  test("проверяет каталог, размер и schema через открытый descriptor", async () => {
    const directory = await protectedDirectory();
    const path = join(directory, "owner.json");
    await writeFile(path, JSON.stringify([]), { mode: 0o600 });
    expect(() => readStorageState(path)).toThrow("schema");

    await writeFile(path, "x".repeat((1 << 20) + 1), { mode: 0o600 });
    expect(() => readStorageState(path)).toThrow("size");

    await chmod(directory, 0o755);
    expect(() => readStorageState(path)).toThrow("0700");
  });

  test("не принимает API cookies из bootstrap state", async () => {
    const directory = await protectedDirectory();
    const path = join(directory, "owner.json");
    await writeFile(
      path,
      JSON.stringify({
        cookies: [cookie("__Host-kodex-session", "api")],
        origins: [],
      }),
      { mode: 0o600 },
    );
    expect(() => readStorageState(path)).toThrow("Kodex API cookie");
  });
});

async function protectedDirectory(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "kodex-storage-state-"));
  directories.push(root);
  const directory = join(root, "auth");
  await mkdir(directory, { mode: 0o700 });
  return directory;
}

function cookie(name: string, value: string) {
  return {
    name,
    value,
    domain: "kodex.example.test",
    path: "/",
    expires: 1_800_000_000,
    httpOnly: true,
    secure: true,
    sameSite: "Lax" as const,
  };
}
