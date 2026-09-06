import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import { serverMessageTokens } from "./server-message-catalog";
import {
  serverPermissionKeys,
  serverPermissionTokens,
} from "./permission-message-catalog";

const ownerRoot = fileURLToPath(
  new URL("../../../../../internal/control-plane/internal/", import.meta.url),
);
const source = (path: string) => readFileSync(resolve(ownerRoot, path), "utf8");

function closedCases(path: string, functionName: string): string[] {
  const block = source(path).match(
    new RegExp(`func ${functionName}\\([^]*?\\n}`),
  )?.[0];
  expect(block, functionName).toBeTruthy();
  return [...(block ?? "").matchAll(/case ([^:]+):/g)].flatMap((match) =>
    [...(match[1] ?? "").matchAll(/"([A-Z][A-Z0-9_]+)"/g)].map(
      (value) => value[1] ?? "",
    ),
  );
}

describe("полнота закрытого реестра server tokens", () => {
  it("покрывает literal tokens исполняемого владельца, включая SQL", () => {
    const required = new Set<string>();
    for (const entry of readdirSync(ownerRoot, {
      recursive: true,
      withFileTypes: true,
    })) {
      if (
        !entry.isFile() ||
        !/\.(go|sql)$/.test(entry.name) ||
        entry.name.endsWith("_test.go") ||
        entry.parentPath.includes("/testdata")
      )
        continue;
      for (const match of readFileSync(
        resolve(entry.parentPath, entry.name),
        "utf8",
      ).matchAll(/\bi18n:([A-Z][A-Z0-9_]*)/g))
        required.add(match[1] ?? "");
    }
    expect(required.size).toBeGreaterThan(100);
    expect(
      [...required].filter((key) => !serverMessageTokens.has(key)),
    ).toEqual([]);
    expect(serverMessageTokens.has("SYSTEM_BASE_ROLE_IMAGE")).toBe(true);
    expect(serverMessageTokens.has("CONFIG_OVERLAY_INVALID_OR_PROTECTED")).toBe(
      true,
    );
  });

  it("покрывает dynamic permission и system-role families из закрытых owner registries", () => {
    const permissions = [
      ...source("domain/service/access/engine.go").matchAll(
        /permission\("([a-z.]+)"/g,
      ),
    ].map((match) => match[1] ?? "");
    expect([...serverPermissionKeys].sort()).toEqual(permissions.sort());
    expect(Object.keys(serverPermissionTokens)).toHaveLength(
      permissions.length * 2,
    );
    const roles = [
      ...source("repository/postgres/platform/access.go").matchAll(
        /name:\s*"i18n:(SYSTEM_ROLE_[A-Z]+)"/g,
      ),
    ].map((match) => match[1] ?? "");
    expect(roles).toHaveLength(5);
    expect(
      roles.filter((key) => !serverMessageTokens.has(`${key}_DESCRIPTION`)),
    ).toEqual([]);
  });

  it("покрывает dynamic safe error families без произвольного runtime regexp", () => {
    const required = [
      ...closedCases(
        "repository/postgres/platform/runtime.go",
        "runtimeSafeErrorCode",
      ),
      ...closedCases(
        "repository/postgres/platform/workers.go",
        "safeIntegrationErrorCode",
      ),
    ];
    expect(required.length).toBeGreaterThan(20);
    expect(required.filter((key) => !serverMessageTokens.has(key))).toEqual([]);
  });

  it.each(["ru", "en"] as const)(
    "каждый зарегистрированный token имеет явный перевод в %s",
    (locale) => {
      for (const token of serverMessageTokens) {
        const key = `serverMessages.${token}`;
        expect(i18n.global.te(key, locale), key).toBe(true);
        const value = i18n.global.t(key, {}, { locale });
        expect(value.trim(), key).not.toBe("");
        expect(value, key).not.toBe(key);
        expect(value, key).not.toContain("i18n:");
      }
    },
  );
});
