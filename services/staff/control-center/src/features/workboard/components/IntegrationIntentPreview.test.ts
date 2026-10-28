import { renderToString } from "@vue/server-renderer";
import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { expect, it, vi } from "vitest";
import { i18n as applicationI18n } from "@/app/i18n";
import type { IntegrationIntent } from "@/shared/api/generated/openapi/types.gen";
import IntegrationIntentPreview from "./IntegrationIntentPreview.vue";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

function intent(): IntegrationIntent {
  return {
    connectionRef: "int_preview",
    connectionName: "Техническая QA оснастка",
    definitionKey: "synthetic",
    operation: "synthetic.journal.write",
    capabilityKey: "synthetic.journal.write",
    effectKey: "effect_preview",
    resourceScope: {
      kind: "SYNTHETIC_JOURNAL",
      values: { journal: "qa1830-exact-journal" },
      digest: "a".repeat(64),
    },
    effectPreview: {
      risk: "WRITE",
      approvalPolicy: "HUMAN_EACH_EFFECT",
      contentComplete: true,
      fields: [
        {
          key: "action",
          type: "STRING",
          value: "CREATE",
          opaque: false,
          truncated: false,
        },
        {
          key: "value",
          type: "STRING",
          value: "Точное содержимое QA",
          opaque: false,
          truncated: false,
        },
        {
          key: "fault",
          type: "STRING",
          value: "RETRYABLE_ONCE",
          opaque: false,
          truncated: false,
        },
      ],
    },
  };
}

async function render(
  value: IntegrationIntent,
  locale: "ru" | "en" = "ru",
): Promise<string> {
  return renderToString(
    createSSRApp(IntegrationIntentPreview, { intent: value }).use(
      createI18n({
        legacy: false,
        locale,
        messages: { [locale]: applicationI18n.global.getLocaleMessage(locale) },
      }),
    ),
  );
}

it.each(["ru", "en"] as const)(
  "точные generic поля и scope видны вне закрытых технических деталей (%s)",
  async (locale) => {
    const html = await render(intent(), locale);
    expect(html).toContain(
      locale === "ru" ? "Область ресурсов" : "Resource scope",
    );
    expect(html).not.toContain("integrationsRedesign.resourceScope");
    const primary = html.replace(/<details\b[^]*?<\/details>/g, "");
    for (const value of [
      "CREATE",
      "Точное содержимое QA",
      "RETRYABLE_ONCE",
      "qa1830-exact-journal",
    ])
      expect(primary).toContain(value);
    expect(primary).not.toContain("effect_preview");
    expect(primary).not.toContain("synthetic.journal.write");
    expect(html).not.toContain('role="status"');
  },
);

it("opaque/private/неизвестный raw preview не раскрывается, неполнота видна", async () => {
  const value = intent();
  value.effectPreview.fields = [
    {
      key: "value",
      type: "STRING",
      value: "OPAQUE_SENTINEL",
      opaque: true,
      truncated: false,
    },
    {
      key: "authorization",
      type: "STRING",
      value: "PRIVATE_SENTINEL",
      opaque: false,
      truncated: false,
    },
  ];
  value.effectPreview.rawInput = "RAW_SENTINEL";
  const html = await render(value);
  expect(html).not.toMatch(/OPAQUE_SENTINEL|PRIVATE_SENTINEL|RAW_SENTINEL/);
  expect(html).toContain("Значение скрыто в безопасном описании");
  expect(html).toContain("Безопасное описание действия неполное");
});

it("truncated и отсутствующий safe preview не превращаются в полное описание", async () => {
  const value = intent();
  value.effectPreview.fields = [
    {
      key: "value",
      type: "STRING",
      value: "Доступный префикс",
      opaque: false,
      truncated: true,
    },
  ];
  const html = await render(value);
  expect(html).toContain("Доступный префикс");
  expect(html).toContain("Показана только часть значения");
  expect(html).toContain("Безопасное описание действия неполное");
  value.effectPreview = { risk: "DESTRUCTIVE" };
  const missing = await render(value);
  expect(missing).toContain("необратимое изменение");
  expect(missing).toContain("Безопасное описание действия неполное");
});

it("HUMAN_SCOPED сохраняет selected и mutable paths; строки остаются буквальными", async () => {
  const value = intent();
  value.effectPreview.approvalPolicy = "HUMAN_SCOPED";
  value.effectPreview.approvalScope = {
    selected: [
      { path: "/journal", type: "STRING", value: "qa1830-exact-journal" },
    ],
    mutablePaths: ["/value"],
  };
  value.effectPreview.fields = [
    {
      key: "value",
      type: "STRING",
      value: '<img src=x onerror="unexpected()"> i18n:SECRET',
      opaque: false,
      truncated: false,
    },
    {
      key: "count",
      type: "INTEGER",
      value: 0,
      opaque: false,
      truncated: false,
    },
    {
      key: "enabled",
      type: "BOOLEAN",
      value: false,
      opaque: false,
      truncated: false,
    },
  ];
  const html = await render(value);
  const scope =
    /<section class="decision-approval-scope"[^]*?<\/section>/.exec(
      html,
    )?.[0] ?? "";
  expect(scope).toContain("qa1830-exact-journal");
  expect(scope).toContain("journal");
  expect(scope).toContain("value");
  expect(html).toContain("&lt;img");
  expect(html).not.toContain("<img src=x");
  expect(html).toContain("i18n:SECRET");
  expect(html).toContain(">0</span>");
  expect(html).toContain(">false</span>");
});

it("body1841байт целиком находится в отдельной текстовой строке рядом с Issue number", async () => {
  const value = intent();
  const prefix = "Строка комментария. ".repeat(40);
  const body =
    prefix + "x".repeat(1841 - new TextEncoder().encode(prefix).length);
  expect(new TextEncoder().encode(body)).toHaveLength(1841);
  value.effectPreview.fields = [
    {
      key: "issue_number",
      type: "INTEGER",
      value: 17,
      opaque: false,
      truncated: false,
    },
    {
      key: "body",
      type: "STRING",
      value: body,
      opaque: false,
      truncated: false,
    },
  ];
  const html = await render(value);
  // Проверяем настоящий рендер: wide-строка назначается тексту, а не Issue number.
  const row =
    /<div class="decision-effect-field--text"[^]*?<\/div>/.exec(html)?.[0] ??
    "";
  expect(row).toContain(body);
  expect(row).not.toContain("issue_number");
});

it.each(["ru", "en"] as const)(
  "SSH Gate показывает exact release и public scope, не private custody (%s)",
  async (locale) => {
    const value = intent();
    value.definitionKey = "ssh-compose";
    value.operation = value.capabilityKey = "ssh.compose.deploy";
    value.resourceScope = {
      kind: "SSH_TARGET",
      digest: "a".repeat(64),
      values: {
        project_ref: "prj_ssh_fixture",
        owner: "example",
        repository: "project",
        target_ref: "ssh_immutable_fixture",
        address: "8.8.8.8",
        user: "deployer",
        host_key: "ssh-ed25519 public-fixture",
        published_port: "8080",
        runtime_image: `registry.example/qa/app@sha256:${"d".repeat(64)}`,
      },
    };
    const release = {
      merged_sha: "b".repeat(40),
      tree_sha: "c".repeat(40),
      bundle_sha256: "e".repeat(64),
      runtime_image: value.resourceScope.values.runtime_image,
    };
    value.effectPreview.fields = Object.entries(release).map(([key, text]) => ({
      key,
      type: "STRING",
      value: text,
      bytes: new TextEncoder().encode(text).length,
      opaque: false,
      truncated: false,
    }));
    const html = await render(value, locale);
    const primary = html.replace(/<details\b[^]*?<\/details>/g, "");
    for (const pin of Object.values(release)) expect(primary).toContain(pin);
    expect(primary).toContain("ssh_immutable_fixture");
    expect(primary).toContain("8.8.8.8");
    expect(primary).not.toContain("decisions.integrationFieldLabels.");
    for (const privateKey of [
      "sshSecret",
      "originRuntimeRevisionId",
      "originLeaseId",
      "secretUid",
    ])
      expect(html).not.toContain(privateKey);
    expect(primary).not.toContain('role="status"');
  },
);
