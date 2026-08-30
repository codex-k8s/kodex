import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

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
      syncFailed: "Не удалось подготовить набор вложений",
      progress: "Подготовлено {uploaded} из {count}",
      aggregateLimit: "Превышен лимит",
      existing: {
        choose: "Выбрать загруженные",
        title: "Доступные файлы",
        hint: "Поиск выполняется на сервере",
        label: "Выбор загруженных файлов",
        search: "Найти файл",
        loading: "Загружаем файлы",
        loadingMore: "Загружаем ещё",
        empty: "Доступных проверенных файлов нет",
        error: "Не удалось загрузить доступные файлы",
        attached: "Добавлен во вложения",
        detachHint: "Файл останется в хранилище",
      },
      states: {
        QUEUED: "В очереди",
        UPLOADING: "Загружается",
        SCANNING: "Проверяется",
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
          purpose: "RUN_INPUT",
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
    expect(html).toContain("Выбрать загруженные");
    expect(html).not.toContain("Удалить файл");
  });

  it("оставляет вложения доступными глобальному assistant без Project", async () => {
    const app = createSSRApp({
      render: () =>
        h(AttachmentComposer, {
          purpose: "ASSISTANT_MESSAGE",
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

    expect(html).toContain("Добавить файлы");
    expect(html).toContain("Выбрать загруженные");
    expect(html).not.toContain("disabled");
  });
});
