import { describe, expect, it } from "vitest";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { integrationInteger, readIntegerBounds } from "./integer-bounds";
import { prepareConnectionConfiguration } from "./connection-setup";
import IntegrationIntegerBounds from "./ui/IntegrationIntegerBounds.vue";
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";

const field: IntegrationConfigurationField = {
  key: "pipeline_id",
  label: "Pipeline",
  help: "",
  required: false,
  valueType: "INTEGER",
  minimum: "-9223372036854775808",
  maximum: "9223372036854775807",
};

describe("точные целочисленные границы интеграций", () => {
  it("сохраняет обе int64 границы и safe JSON payload", () => {
    expect(readIntegerBounds(field)).toEqual({
      valid: true,
      minimum: -9223372036854775808n,
      maximum: 9223372036854775807n,
    });
    for (const value of ["-9007199254740991", "0", "9007199254740991"])
      expect(
        prepareConnectionConfiguration([field], { pipeline_id: value }),
      ).toMatchObject({ value: { pipeline_id: Number(value) }, problems: {} });
    for (const value of [
      "9007199254740992",
      "-9007199254740992",
      "9223372036854775807",
      "-9223372036854775808",
    ])
      expect(
        prepareConnectionConfiguration([field], { pipeline_id: value })
          .problems,
      ).toEqual({ pipeline_id: "INVALID_VALUE" });
  });

  it("сравнивает равенство и соседние значения без округления", () => {
    const bounds = readIntegerBounds({
      minimum: 9007199254740990,
      maximum: "9007199254740992",
    });
    expect(integrationInteger("9007199254740989", bounds)).toBeUndefined();
    expect(integrationInteger("9007199254740990", bounds)).toBe(
      9007199254740990,
    );
    expect(integrationInteger("9007199254740991", bounds)).toBe(
      9007199254740991,
    );
    const negative = readIntegerBounds({
      minimum: "-9007199254740992",
      maximum: -9007199254740990,
    });
    expect(integrationInteger("-9007199254740991", negative)).toBe(
      -9007199254740991,
    );
    expect(integrationInteger("-9007199254740989", negative)).toBeUndefined();
    expect(
      readIntegerBounds({
        minimum: "9223372036854775807",
        maximum: "9223372036854775806",
      }),
    ).toEqual({ valid: false });
  });

  it.each([
    "+1",
    "01",
    "-0",
    "1e3",
    "1.5",
    " 1",
    "9223372036854775808",
    "-9223372036854775809",
    9007199254740992,
    1.5,
    NaN,
    Infinity,
  ])(
    "закрыто отклоняет некорректную metadata %s даже при пустом поле",
    (maximum) => {
      const invalid = { ...field, maximum };
      expect(readIntegerBounds(invalid)).toEqual({ valid: false });
      expect(prepareConnectionConfiguration([invalid], {}).problems).toEqual({
        pipeline_id: "INVALID_VALUE",
      });
    },
  );

  it("сохраняет обычные диапазоны и отклоняет дробные значения", () => {
    const bounds = readIntegerBounds({ minimum: 1, maximum: 10 });
    expect(integrationInteger("1", bounds)).toBe(1);
    expect(integrationInteger("10", bounds)).toBe(10);
    for (const value of ["0", "11", "1.5", "1e1"])
      expect(integrationInteger(value, bounds)).toBeUndefined();
  });

  it("показывает точные int64 границы в интерфейсе", async () => {
    const app = createSSRApp({
      render: () => h(IntegrationIntegerBounds, { field }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "en",
        messages: {
          en: { integrations: { minimum: "Minimum", maximum: "Maximum" } },
        },
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain("Minimum: -9223372036854775808");
    expect(html).toContain("Maximum: 9223372036854775807");
    expect(html).not.toContain("9223372036854776000");
  });
});
