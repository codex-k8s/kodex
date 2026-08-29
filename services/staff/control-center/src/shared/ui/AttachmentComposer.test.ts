import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";

import AttachmentComposer from "@/shared/ui/AttachmentComposer.vue";

const messages = {
  ru: {
    common: { retry: "Повторить" },
    attachments: {
      add: "Добавить файлы",
      dropHint: "или перетащите сюда",
      drop: "Отпустите файлы для загрузки",
      remove: "Убрать загруженный файл «{name}» из вложений",
      detach: "Убрать файл «{name}» из текущих вложений",
      retry: "Повторить загрузку файла «{name}»",
      uploading: "Загружается файл «{name}»",
      uploadFailed: "Не удалось загрузить файл",
      progress: "Подготовлено {uploaded} из {count}",
      aggregateLimit: "Превышен лимит",
      existing: {
        choose: "Выбрать из Проекта",
        title: "Файлы Проекта",
        hint: "Поиск выполняется на сервере",
        label: "Выбор файлов Проекта",
        search: "Найти файл в Проекте",
        loading: "Загружаем файлы",
        loadingMore: "Загружаем ещё",
        empty: "Доступных проверенных файлов нет",
        error: "Не удалось загрузить файлы Проекта",
        attached: "Добавлен во вложения",
        detachHint: "Файл останется в Проекте",
      },
      states: {
        QUEUED: "В очереди",
        UPLOADING: "Загружается",
        UPLOADED: "Готов",
        FAILED: "Ошибка",
      },
    },
  },
};

describe("AttachmentComposer", () => {
  it("показывает единый drop surface и выбор существующих Project-файлов", async () => {
    const app = createSSRApp({
      render: () =>
        h(AttachmentComposer, {
          projectRef: "project_1",
          upload: vi.fn(),
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages,
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain('class="attachment-composer');
    expect(html).toContain("Добавить файлы");
    expect(html).toContain("или перетащите сюда");
    expect(html).toContain("Выбрать из Проекта");
    expect(html).not.toContain("Удалить файл");
  });
});
