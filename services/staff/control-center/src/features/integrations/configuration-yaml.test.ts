import { describe, expect, it } from "vitest";
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import { connectionYaml, parseConnectionYaml } from "./configuration-yaml";
import { prepareConnectionConfiguration } from "./connection-setup";

const fields: IntegrationConfigurationField[] = [
  {
    key: "host",
    label: "Host",
    help: "",
    valueType: "URL",
    format: "HTTPS_ORIGIN",
    required: true,
  },
  {
    key: "port",
    label: "Port",
    help: "",
    valueType: "INTEGER",
    minimum: 1,
    maximum: 65535,
    required: true,
  },
  {
    key: "enabled",
    label: "Enabled",
    help: "",
    valueType: "BOOLEAN",
    required: true,
  },
  {
    key: "mode",
    label: "Mode",
    help: "",
    valueType: "TEXT",
    allowedValues: ["TLS", "STARTTLS"],
    maximumLength: 8,
    required: true,
  },
  {
    key: "folders",
    label: "Folders",
    help: "",
    valueType: "STRING_LIST",
    required: false,
  },
];
const values = {
  host: "https://mail.example.invalid",
  port: "993",
  enabled: "false",
  mode: "TLS",
  folders: "INBOX, Sent",
};

describe("публичная конфигурация подключения", () => {
  it("сохраняет типы и значения при round-trip формы/YAML", () => {
    const source = connectionYaml(fields, values);
    expect(parseConnectionYaml(source, fields)).toEqual(values);
    expect(prepareConnectionConfiguration(fields, values)).toEqual({
      value: {
        host: values.host,
        port: 993,
        enabled: false,
        mode: "TLS",
        folders: ["INBOX", "Sent"],
      },
      problems: {},
    });
  });
  it("закрыто отклоняет неизвестные поля и ошибочные типы без утечки исходного значения", () => {
    const source = connectionYaml(fields, values);
    for (const extra of [
      "password: test-only-private",
      "unknown: value",
      "__proto__: {}",
      "port: '993'",
      "enabled: 'false'",
      "folders: [12]",
    ]) {
      expect(() =>
        parseConnectionYaml(`${source}\n${extra}`, fields),
      ).toThrow();
    }
    expect(() =>
      parseConnectionYaml(`${source}\npassword: test-only-private`, fields),
    ).toThrow("Unknown or protected connection configuration field");
  });
  it("проверяет limits, enum и HTTPS origin без userinfo", () => {
    for (const change of [
      { port: "0" },
      { port: "65536" },
      { port: "1.5" },
      { port: "1e2" },
      { enabled: "yes" },
      { mode: "PLAIN" },
      { host: "https://user:password@example.invalid" },
      { host: "https://example.invalid/path" },
      { host: "https://example.invalid?token=value" },
    ]) {
      expect(
        Object.keys(
          prepareConnectionConfiguration(fields, { ...values, ...change })
            .problems,
        ),
      ).not.toHaveLength(0);
      expect(() => connectionYaml(fields, { ...values, ...change })).toThrow();
    }
  });
});
