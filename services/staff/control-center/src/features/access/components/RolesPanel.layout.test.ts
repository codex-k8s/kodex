import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";

import RolesPanel from "@/features/access/components/RolesPanel.vue";
import type { AccessRole } from "@/shared/api/generated/openapi/types.gen";

const messages = {
  ru: {
    common: { edit: "Изменить", archive: "Архивировать" },
    access: {
      rolesWorkspace: {
        title: "Роли",
        subtitle: "Назначение полномочий",
        create: "Создать роль",
        search: "Поиск ролей",
        searchPlaceholder: "Найти роль по названию или назначению",
        empty: "Роли не найдены",
        emptyHint: "Создайте роль.",
        searchEmpty: "Подходящие роли не найдены",
        searchEmptyHint: "Измените запрос.",
        bindingsShort: "привязок",
        permissionCount: "Полномочий: {count}",
        showPermissions: "Полномочия и риск ({count})",
        systemImmutable: "Системная роль",
      },
      roleKinds: { CUSTOM: "Пользовательские", SYSTEM: "Системные" },
      scope: { values: { ORGANIZATION: "Организация" } },
      permissionsRegistry: {},
    },
  },
};

describe("RolesPanel", () => {
  it("показывает серверный поиск и ноль для отсутствующего bindingCount", async () => {
    const role = {
      ref: "role_editor",
      version: 1,
      kind: "CUSTOM",
      state: "ACTIVE",
      currentVersion: {
        ref: "role_version_editor",
        roleRef: "role_editor",
        revision: 1,
        name: "Редактор",
        description: "Редактирует материалы",
        permissionKeys: [],
        allowedScopes: ["ORGANIZATION"],
        changeComment: "",
        createdAt: "2026-09-26T00:00:00Z",
        createdBy: {
          ref: "usr_owner",
          displayName: "Владелец",
          emailMasked: "o***@example.com",
        },
      },
      updatedAt: "2026-09-26T00:00:00Z",
    } as unknown as AccessRole;
    const app = createSSRApp({
      render: () =>
        h(RolesPanel, {
          roles: [role],
          permissions: [],
          hasMore: false,
        }),
    });
    app.use(
      createI18n({ legacy: false, locale: "ru", messages, missingWarn: false }),
    );

    const html = await renderToString(app);

    expect(html).toContain('name="access-role-search"');
    expect(html).toContain("Найти роль по названию или назначению");
    expect(html).toContain("v1 · 0 привязок");
  });
});
