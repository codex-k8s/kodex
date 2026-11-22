import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";
import type { KubernetesArtifactCaptureOrigin as CaptureOrigin } from "@/shared/api/generated/openapi/types.gen";
import { captureOriginFixture } from "@/shared/api/kubernetes-capture-origin.fixture";
import { fileRevisionMessages } from "./revision-messages";
import KubernetesCaptureOrigin from "./KubernetesCaptureOrigin.vue";

/** Отрисовывает штатный Vue component с настоящими переводами без нового DOM adapter. */
async function render(origin: CaptureOrigin): Promise<string> {
  const app = createSSRApp({
    render: () => h(KubernetesCaptureOrigin, { origin }),
  });
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: { ru: { fileRevision: fileRevisionMessages.ru } },
    }),
  );
  return renderToString(app);
}

describe("сохранённый Kubernetes результат в истории файла", () => {
  it("показывает неполноту отдельно от remote exit 0 и не утверждает остановку", async () => {
    const html = await render(captureOriginFixture());
    expect(html).toContain("Сохранена подтверждённая часть вывода");
    expect(html).toContain("Код завершения: 0");
    expect(html).toContain(
      "Наличие файла не означает успешное выполнение инструмента",
    );
    expect(html).not.toContain("Сохранён весь допущенный вывод");
  });
  it("закрыто отображает неверный FULL вместо ложной полноты", async () => {
    const origin = captureOriginFixture();
    origin.kind = "FULL";
    const html = await render(origin);
    expect(html).toContain('role="alert"');
    expect(html).toContain("не подтверждено");
    expect(html).not.toContain("весь допущенный вывод");
  });
});
