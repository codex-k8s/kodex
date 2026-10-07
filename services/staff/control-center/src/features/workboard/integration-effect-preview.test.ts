import { describe, expect, it } from "vitest";
import type { IntegrationIntent } from "@/shared/api/generated/openapi/types.gen";
import { integrationEffectPreview } from "./integration-effect-preview";

const intent = (
  fields: unknown[],
  contentComplete = true,
): IntegrationIntent => ({
  connectionRef: "intconn_fixture",
  connectionName: "Fixture GitHub",
  definitionKey: "github",
  operation: "github.issue.comment.create",
  capabilityKey: "github.issue.comment.create",
  resourceScope: {
    kind: "GITHUB_REPOSITORY",
    values: { owner: "fixture-owner", repository: "fixture-repo" },
    digest: "a".repeat(64),
  },
  effectPreview: {
    contentComplete,
    fields,
    inputDigest: "b".repeat(64),
    inputBytes: 42,
  },
  effectKey: "effect_fixture",
});

describe("integrationEffectPreview", () => {
  it("читает actual typed key fields, отдельно от approval JSON pointers", () => {
    const value = intent([
      { key: "issue_number", type: "INTEGER", value: 17, opaque: false },
      {
        key: "body",
        type: "STRING",
        value: "Точный комментарий\nвторая строка",
        opaque: false,
        truncated: false,
      },
      { key: "enabled", type: "BOOLEAN", value: false, opaque: false },
    ]);
    const before = structuredClone(value);
    expect(integrationEffectPreview(value)).toMatchObject({
      complete: true,
      repositoryName: "fixture-owner/fixture-repo",
      commentIssue: 17,
      fields: [
        { key: "issue_number", value: 17, hidden: false },
        {
          key: "body",
          value: "Точный комментарий\nвторая строка",
          hidden: false,
        },
        { key: "enabled", value: false, hidden: false },
      ],
    });
    expect(value).toEqual(before);
  });

  it.each([
    "password",
    "authorization",
    "headers",
    "cookie",
    "secret",
    "token",
    "credential",
    "content_base64",
    "attachments",
    "workflow_inputs",
  ])("не выводит значение %s даже при некорректном opaque=false", (key) => {
    const preview = integrationEffectPreview(
      intent([
        {
          key,
          type: "STRING",
          value: "PRIVATE_VALUE_SENTINEL",
          opaque: false,
          truncated: false,
        },
      ]),
    );
    expect(preview.complete).toBe(false);
    expect(preview.fields).toEqual([{ key, hidden: true, truncated: false }]);
    expect(JSON.stringify(preview)).not.toContain("PRIVATE_VALUE_SENTINEL");
  });

  it.each([
    { key: "body", type: "STRING", value: "HIDDEN_SENTINEL", opaque: true },
    { key: "body", type: "STRING", value: "HIDDEN_SENTINEL" },
    {
      key: "body",
      type: "OBJECT",
      value: { value: "HIDDEN_SENTINEL" },
      opaque: false,
    },
    { key: "body", type: "INTEGER", value: "HIDDEN_SENTINEL", opaque: false },
    {
      key: "body",
      type: "STRING",
      value: { value: "HIDDEN_SENTINEL" },
      opaque: false,
    },
  ])(
    "закрыто скрывает opaque, missing flag и неверный scalar type",
    (field) => {
      const preview = integrationEffectPreview(intent([field]));
      expect(preview.complete).toBe(false);
      expect(preview.fields[0]?.hidden).toBe(true);
      expect(JSON.stringify(preview)).not.toContain("HIDDEN_SENTINEL");
    },
  );

  it("показывает только bounded UTF-8 prefix и не выдаёт его за полное содержимое", () => {
    const preview = integrationEffectPreview(
      intent([
        {
          key: "body",
          type: "STRING",
          value: "я".repeat(3000),
          opaque: false,
          truncated: false,
        },
      ]),
    );
    expect(preview.complete).toBe(false);
    expect(preview.fields[0]).toMatchObject({
      value: "я".repeat(2048),
      hidden: false,
      truncated: true,
    });
  });

  it("сохраняет server truncated/contentComplete warning и не угадывает absent completeness", () => {
    const value = intent([
      {
        key: "body",
        type: "STRING",
        value: "часть",
        opaque: false,
        truncated: true,
      },
    ]);
    expect(integrationEffectPreview(value)).toMatchObject({
      complete: false,
      fields: [{ truncated: true }],
    });
    expect(
      integrationEffectPreview(
        intent(
          [
            {
              key: "body",
              type: "STRING",
              value: "полное",
              opaque: false,
              truncated: false,
            },
          ],
          false,
        ),
      ).complete,
    ).toBe(false);
    delete value.effectPreview.contentComplete;
    expect(integrationEffectPreview(value).complete).toBe(false);
    expect(integrationEffectPreview(undefined).complete).toBe(false);
  });

  it("ограничивает суммарный text preview 16KiB и число полей64", () => {
    const preview = integrationEffectPreview(
      intent(
        Array.from({ length: 70 }, (_, index) => ({
          key: `field_${String(index)}`,
          type: "STRING",
          value: "x".repeat(4096),
          opaque: false,
          truncated: false,
        })),
      ),
    );
    expect(preview.fields).toHaveLength(64);
    expect(preview.complete).toBe(false);
    expect(
      preview.fields.reduce(
        (sum, field) => sum + String(field.value ?? "").length,
        0,
      ),
    ).toBe(16384);
  });

  it("не выбирает произвольного winner при дубликате одного typed key", () => {
    const preview = integrationEffectPreview(
      intent([
        {
          key: "body",
          type: "STRING",
          value: "FIRST_SENTINEL",
          opaque: false,
          truncated: false,
        },
        {
          key: "body",
          type: "STRING",
          value: "SECOND_SENTINEL",
          opaque: false,
          truncated: false,
        },
      ]),
    );
    expect(preview.fields).toEqual([
      { key: "body", hidden: true, truncated: false },
    ]);
    expect(preview.complete).toBe(false);
  });

  it("technical projection не содержит raw fields/headers/неизвестные payloads", () => {
    const value = intent([
      {
        key: "body",
        type: "STRING",
        value: "SAFE_COMMENT",
        opaque: false,
        truncated: false,
      },
    ]);
    value.effectPreview.headers = { authorization: "HEADER_SENTINEL" };
    value.effectPreview.extra = "EXTRA_SENTINEL";
    value.effectPreview.approvalPolicy = "HUMAN_EACH_EFFECT";
    const preview = integrationEffectPreview(value);
    expect(preview.technical).toEqual({
      inputDigest: "b".repeat(64),
      inputBytes: 42,
      contentComplete: true,
      approvalPolicy: "HUMAN_EACH_EFFECT",
    });
    expect(JSON.stringify(preview)).not.toMatch(
      /HEADER_SENTINEL|EXTRA_SENTINEL/,
    );
  });
});
