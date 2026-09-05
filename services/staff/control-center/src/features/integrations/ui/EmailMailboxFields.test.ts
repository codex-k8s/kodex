import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { expect, it } from "vitest";
import type { EmailMailboxSpecification } from "@/shared/api/generated/openapi/types.gen";
import EmailMailboxFields from "./EmailMailboxFields.vue";

async function render(value: EmailMailboxSpecification, disabled = false) {
  const app = createSSRApp({
    render: () =>
      h(EmailMailboxFields, {
        value,
        disabled,
        credentials: [1, 2].map((generation) => ({
          name: "same-name",
          generation,
          kind: "AUTH_SECRET" as const,
          connectionRef: "connection",
          connectionVersion: 3,
        })),
      }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      missingWarn: false,
      fallbackWarn: false,
    }),
  );
  return renderToString(app);
}
it("различает поколения credential и сохраняет отсутствующую exact ссылку", async () => {
  const html = await render({
    receiveProtocol: "IMAP",
    smtp: { secret: { name: "same-name", generation: 3 } },
  });
  expect(html).toContain("same-name · 1");
  expect(html).toContain("same-name · 2");
  expect(html).toContain("same-name · 3");
  expect(html).toContain("mailbox.notInPage");
  expect(html).toContain('aria-label="SMTP"');
  expect(html).toContain('aria-label="IMAP"');
  expect(html).not.toContain('aria-label="POP"');
});
it("POP3 сохраняет SMTP и закрывает все поля при readonly/busy", async () => {
  const html = await render({ receiveProtocol: "POP3", policies: [{}] }, true);
  expect(html).toContain('aria-label="SMTP"');
  expect(html).toContain('aria-label="POP"');
  expect(html).not.toContain('aria-label="IMAP"');
  expect(html).toMatch(/<fieldset[^>]*disabled/);
  expect(html).not.toContain('type="password"');
  expect(html).not.toContain("voice-input-button");
});
