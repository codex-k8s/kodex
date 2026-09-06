import { describe, expect, it } from "vitest";
import {
  parseConfigurationDocument,
  serializeConfigurationDocument,
  normalizedConfigurationDocument,
} from "./document";
describe("managed configuration document", () => {
  it("нормализует порядок ключей и YAML без перестановки массивов", () => {
    const expected = normalizedConfigurationDocument(
      '{"b":[2,1],"a":{"z":true,"c":null}}',
      "JSON",
    );
    expect(
      normalizedConfigurationDocument(
        "a:\n  c: null\n  z: true\nb: [2, 1]",
        "YAML",
      ),
    ).toBe(expected);
    expect(
      normalizedConfigurationDocument(
        '{"a":{"z":true,"c":null},"b":[1,2]}',
        "JSON",
      ),
    ).not.toBe(expected);
    expect(() =>
      normalizedConfigurationDocument("a: 1\na: 2", "YAML"),
    ).toThrow();
  });
  it("сохраняет typed definition при преобразовании JSON/YAML", () => {
    const value = {
      name: "Тест",
      definition: {
        key: "test",
        version: "1",
        operations: [
          { key: "read", risk: "READ", approval: "HUMAN_EACH_EFFECT" },
        ],
      },
    };
    for (const format of ["JSON", "YAML"] as const)
      expect(
        parseConfigurationDocument(
          serializeConfigurationDocument(value, format),
          format,
        ),
      ).toEqual(value);
  });
  it.each([
    "name: a\nname: b",
    "name: a\n---\nname: b",
    "name: !!js/function function(){}",
    "name: &self { child: *self }",
    "[a, b]",
    "name: .inf",
  ])("отклоняет поврежденный YAML без частичной модели", (value) => {
    expect(() => parseConfigurationDocument(value, "YAML")).toThrow();
  });
});
