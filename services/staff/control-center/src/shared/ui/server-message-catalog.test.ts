import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "vue/server-renderer";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import { serverMessageTokens } from "./server-message-catalog";
import { serverMessageKey } from "./server-message";
import SafeSummary from "./SafeSummary.vue";
import SafeMarkdown from "./SafeMarkdown.vue";
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
  it.each([
    ["ru", "Не удалось подготовить файлы результата"],
    ["en", "Could not prepare result files"],
  ] as const)(
    "показывает отказ файлов результата, а не ошибку ответа модели, в %s",
    async (locale, expected) => {
      const key = "serverMessages.RUNTIME_ARTIFACT_INVALID";
      expect(serverMessageKey("i18n:RUNTIME_ARTIFACT_INVALID")).toBe(key);
      expect(i18n.global.t(key, {}, { locale })).toBe(expected);
      const unknown = "i18n:RUNTIME_ARTIFACT_INTERNAL_DIAGNOSTIC";
      expect(serverMessageKey(unknown)).toBeUndefined();
      const previous = i18n.global.locale.value;
      i18n.global.locale.value = locale;
      try {
        const app = createSSRApp({
          render: () =>
            h("main", [
              h(SafeSummary, { content: "i18n:RUNTIME_ARTIFACT_INVALID" }),
              h(SafeMarkdown, { content: "i18n:RUNTIME_ARTIFACT_INVALID" }),
              h(SafeSummary, { content: unknown }),
            ]),
        });
        app.use(i18n);
        const html = await renderToString(app);
        expect(html.split(expected)).toHaveLength(3);
        expect(html).toContain(i18n.global.t("serverMessages.unsupported"));
        expect(html).not.toContain("RUNTIME_ARTIFACT");
        expect(html).not.toContain("i18n:");
        expect(html).not.toContain(
          i18n.global.t("serverMessages.PROVIDER_RESPONSE_INVALID"),
        );
      } finally {
        i18n.global.locale.value = previous;
      }
    },
  );
  it("покрывает закрытый terminal outcome runner, включая неподтверждённый результат", () => {
    const runner = readFileSync(
      new URL(
        "../../../../../jobs/agent-runner/internal/codex/parser.go",
        import.meta.url,
      ),
      "utf8",
    );
    const terminal = runner.match(/func TerminalPresentation\([^]*?\n}/)?.[0];
    expect(terminal).toBeTruthy();
    const tokens = [
      ...(terminal ?? "").matchAll(/i18n:([A-Z][A-Z0-9_]*)/g),
    ].map((match) => match[1] ?? "");
    expect(new Set(tokens).size).toBe(7);
    expect(tokens.filter((token) => !serverMessageTokens.has(token))).toEqual(
      [],
    );
  });
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
