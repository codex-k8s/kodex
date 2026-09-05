import { readFileSync, readdirSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  parseConfigurationDocument,
  serializeConfigurationDocument,
} from "./document";
import {
  emptyPackageField,
  packageDiagnostics,
  packageSchema,
  resolvePackageField,
} from "./integration-package";
const directory = new URL(
  "../../../../../../contracts/integrations/v1/definitions/",
  import.meta.url,
);
describe("IntegrationPackage canonical schema", () => {
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
});
