import { describe, expect, it, vi } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve, relative } from "node:path";
import ts from "typescript";
import { parse, compileTemplate } from "vue/compiler-sfc";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "./index";
import integrationPackageSchema from "@/shared/api/generated/integration-package/schema.json";
function entries(value: unknown, prefix = ""): [string, string][] {
  if (typeof value === "string") return [[prefix, value]];
  if (!value || typeof value !== "object") return [];
  return Object.entries(value).flatMap(([key, child]) =>
    entries(child, prefix ? `${prefix}.${key}` : key),
  );
}
describe("Control Center translations", () => {
  it("переводит каждое поле схемы пакета интеграции", () => {
    const keys = new Set<string>();
    const visited = new Set<object>();
    function visit(value: unknown): void {
      if (!value || typeof value !== "object" || visited.has(value)) return;
      visited.add(value);
      if (Array.isArray(value)) {
        value.forEach(visit);
        return;
      }
      const record = value as Record<string, unknown>;
      if (record.properties && typeof record.properties === "object")
        Object.keys(record.properties).forEach((key) => keys.add(key));
      Object.values(record).forEach(visit);
    }
    visit(integrationPackageSchema);
    const missing = [...keys].filter(
      (key) =>
        !i18n.global.te(`managed.packageFields.${key}`, "ru") ||
        !i18n.global.te(`managed.packageFields.${key}`, "en"),
    );
    expect(missing.sort()).toEqual([]);
  });
  it("переводит все состояния узла графа выполнения", () => {
    for (const state of [
      "PLANNED",
      "QUEUED",
      "RUNNING",
      "WAITING",
      "SUCCEEDED",
      "FAILED",
      "CANCELLED",
      "SKIPPED",
    ]) {
      expect(i18n.global.te(`states.${state}`, "ru"), state).toBe(true);
      expect(i18n.global.te(`states.${state}`, "en"), state).toBe(true);
    }
  });
  it("разрешает статические ключи из Vue и TypeScript без показа идентификаторов в UI", () => {
    const root = fileURLToPath(new URL("../../", import.meta.url));
    const keys = new Set(
      entries(i18n.global.getLocaleMessage("ru")).map(([key]) => key),
    );
    const missing = new Set<string>();
    function check(source: string, filename: string): void {
      const ast = ts.createSourceFile(
        filename,
        source,
        ts.ScriptTarget.Latest,
        true,
        ts.ScriptKind.TS,
      );
      function visit(node: ts.Node): void {
        if (ts.isCallExpression(node)) {
          const callee = node.expression;
          const name = ts.isIdentifier(callee)
            ? callee.text
            : ts.isPropertyAccessExpression(callee)
              ? callee.name.text
              : "";
          const arg = node.arguments[0];
          if (
            (name === "t" || name === "$t") &&
            arg &&
            ts.isStringLiteralLike(arg) &&
            !keys.has(arg.text)
          )
            missing.add(`${relative(root, filename)}: ${arg.text}`);
        }
        ts.forEachChild(node, visit);
      }
      visit(ast);
    }
    for (const file of readdirSync(root, {
      recursive: true,
      withFileTypes: true,
    })) {
      if (
        !file.isFile() ||
        !/\.(?:vue|ts)$/.test(file.name) ||
        /\.(?:test|d)\.ts$/.test(file.name)
      )
        continue;
      const filename = resolve(file.parentPath, file.name);
      if (filename.includes("/generated/")) continue;
      const source = readFileSync(filename, "utf8");
      if (file.name.endsWith(".vue")) {
        const parsed = parse(source, { filename });
        expect(parsed.errors, filename).toEqual([]);
        if (parsed.descriptor.script)
          check(parsed.descriptor.script.content, filename);
        if (parsed.descriptor.scriptSetup)
          check(parsed.descriptor.scriptSetup.content, filename);
        if (parsed.descriptor.template) {
          const template = compileTemplate({
            source: parsed.descriptor.template.content,
            filename,
            id: "translation-check",
            compilerOptions: { expressionPlugins: ["typescript"] },
          });
          expect(template.errors, filename).toEqual([]);
          check(template.code, filename);
        }
      } else check(source, filename);
    }
    expect([...missing].sort()).toEqual([]);
  });
  it("имеет явные переводы каждого русского ключа без скрытого fallback в английский интерфейс", () => {
    const ru = entries(i18n.global.getLocaleMessage("ru"));
    const en = entries(i18n.global.getLocaleMessage("en"));
    const englishKeys = new Set(en.map(([key]) => key));
    expect([...englishKeys].sort()).toEqual(ru.map(([key]) => key).sort());
    expect
      .soft(ru.map(([key]) => key).filter((key) => !englishKeys.has(key)))
      .toEqual([]);
    expect
      .soft(
        en
          .filter(
            ([key, value]) =>
              key !== "common.russian" && /[А-Яа-яЁё]/u.test(value),
          )
          .map(([key]) => key),
      )
      .toEqual([]);
  });
});
