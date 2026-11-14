import { readFileSync, readdirSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  parseConfigurationDocument,
  serializeConfigurationDocument,
} from "./document";
import {
  emptyPackageField,
  packageDiagnosticDetails,
  packageDiagnostics,
  packageSchema,
  resolvePackageField,
} from "./integration-package";
const directory = new URL(
  "../../../../../../contracts/integrations/v1/definitions/",
  import.meta.url,
);
describe("IntegrationPackage canonical schema", () => {
  it("сохраняет семантику canonical patterns в JSON Schema u и HTML v", () => {
    const patterns: string[] = [];
    function visit(value: unknown): void {
      if (!value || typeof value !== "object") return;
      for (const [key, child] of Object.entries(value)) {
        if (key === "pattern" && typeof child === "string")
          patterns.push(child);
        else visit(child);
      }
    }
    visit(packageSchema);
    expect(patterns).toHaveLength(8);
    const samples = [
      "a",
      "a-b",
      "a_b",
      "a.b",
      "a--b",
      "a..b",
      "A",
      "-a",
      "a-",
      "a b",
      "a/b",
      "/project_id",
      "/merge_request_iid",
      "/query/issue",
      "/",
      "github.com",
      "1.2.3",
      "0.1.0",
      "v1.2.3",
      "",
      "я",
      "🙂",
      "a\u0000b",
      "a\rb",
      "a\nb",
    ];
    for (const pattern of patterns) {
      const previous = new RegExp(pattern.replaceAll("\\-", "-"), "u");
      const jsonPattern = new RegExp(pattern, "u");
      const htmlPattern = new RegExp(pattern, "v");
      for (const sample of samples) {
        expect(jsonPattern.test(sample), `${pattern}: ${sample}`).toBe(
          previous.test(sample),
        );
        expect(htmlPattern.test(sample), `${pattern}: ${sample}`).toBe(
          previous.test(sample),
        );
      }
    }
  });

  it("применяет Unicode границы display metadata в реальном package validator", () => {
    for (const character of ["Я", "🙂"]) {
      const value = parseConfigurationDocument(
        readFileSync(new URL("sentry.yaml", directory), "utf8"),
        "YAML",
      );
      const spec = value.spec as Record<string, unknown>;
      const fields = spec.configurationFields as Record<string, unknown>[];
      for (const [key, maximum] of [
        ["label", 120],
        ["help", 500],
      ] as const) {
        const original = fields[0]![key];
        fields[0]![key] = character.repeat(maximum);
        expect(packageDiagnostics(value)).toEqual([]);
        for (const invalid of [
          character.repeat(maximum + 1),
          "a\u0000b",
          "a\rb",
          "a\nb",
        ]) {
          fields[0]![key] = invalid;
          expect(packageDiagnostics(value).length).toBeGreaterThan(0);
        }
        fields[0]![key] = original;
      }
    }
  });

  for (const file of readdirSync(directory).filter((name) =>
    name.endsWith(".yaml"),
  )) {
    it(`проверяет canonical ${file} и сохраняет JSON/YAML семантику`, () => {
      const value = parseConfigurationDocument(
        readFileSync(new URL(file, directory), "utf8"),
        "YAML",
      );
      expect(packageDiagnostics(value)).toEqual([]);
      for (const format of ["JSON", "YAML"] as const)
        expect(
          parseConfigurationDocument(
            serializeConfigurationDocument(value, format),
            format,
          ),
        ).toEqual(value);
    });
  }
  it("не подменяет adapter/readiness и не добавляет legacy name/definition", () => {
    const empty = emptyPackageField(packageSchema) as Record<string, unknown>;
    expect(empty.apiVersion).toBe("integrations.kodex.io/v1");
    expect(empty.kind).toBe("IntegrationPackage");
    expect(empty).not.toHaveProperty("name");
    expect(empty).not.toHaveProperty("definition");
    expect(empty.spec).toMatchObject({ adapter: "", readiness: "" });
    expect(packageDiagnostics(empty).length).toBeGreaterThan(0);
    expect(
      packageDiagnostics({ name: "Legacy", definition: {} }).length,
    ).toBeGreaterThan(0);
  });
  it("отклоняет неизвестные credential значения без отражения в diagnostics", () => {
    const value = parseConfigurationDocument(
      readFileSync(new URL("github.yaml", directory), "utf8"),
      "YAML",
    );
    const spec = value.spec as Record<string, unknown>;
    spec.credential = {
      secretKey: "token",
      kind: "TOKEN",
      value: "private-synthetic-value",
    };
    const errors = packageDiagnostics(value);
    expect(errors.length).toBeGreaterThan(0);
    expect(errors.join()).not.toContain("private-synthetic-value");
  });
  it("возвращает безопасные пути полей для дружелюбной диагностики", () => {
    const value = {
      apiVersion: "integrations.kodex.io/v1",
      kind: "IntegrationPackage",
      metadata: { key: "local-preview" },
      spec: { name: "Предпросмотр YAML" },
    };
    const details = packageDiagnosticDetails(value);
    expect(details).toContainEqual({
      path: ["metadata", "version"],
      keyword: "required",
    });
    expect(details).toContainEqual({
      path: ["spec", "adapter"],
      keyword: "required",
    });
    expect(JSON.stringify(details)).not.toContain("Предпросмотр YAML");
    expect(JSON.stringify(details)).not.toContain("local-preview");
  });
  it("соблюдает bounds и conditional destination schema", () => {
    const value = parseConfigurationDocument(
      readFileSync(new URL("github.yaml", directory), "utf8"),
      "YAML",
    );
    const spec = value.spec as Record<string, unknown>;
    spec.networkDestinations = [
      {
        key: "api",
        source: "STATIC",
        configurationField: "url",
        port: 70000,
        tls: "REQUIRED",
      },
    ];
    expect(
      packageDiagnostics(value).some((error) => error.endsWith(": maximum")),
    ).toBe(true);
    expect(
      packageDiagnostics(value).some((error) => error.endsWith(": required")),
    ).toBe(true);
    expect(
      resolvePackageField({ $ref: "#/$defs/field" }).properties?.type?.enum,
    ).toEqual(["STRING", "INTEGER", "BOOLEAN"]);
  });
  it("читает прежний пустой inputFields OpenAPI без изменения ревизии", () => {
    const value = parseConfigurationDocument(
      readFileSync(new URL("openapi-mcp.yaml", directory), "utf8"),
      "YAML",
    );
    const spec = value.spec as Record<string, unknown>;
    const capabilities = spec.capabilities as Record<string, unknown>[];
    const first = capabilities[0];
    if (!first) throw new Error("Missing OpenAPI capability fixture");
    first.inputFields = null;
    expect(packageDiagnostics(value)).toEqual([]);
    expect(first.inputFields).toBeNull();
    spec.adapter = "GITHUB";
    expect(packageDiagnostics(value).length).toBeGreaterThan(0);
  });
});
