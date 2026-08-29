import { createSSRApp } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import CurrentUserSummary from "@/shared/ui/CurrentUserSummary.vue";

describe("CurrentUserSummary", () => {
  it("показывает имя и роль без email и внутреннего ref", async () => {
    const app = createSSRApp(CurrentUserSummary, {
      user: {
        ref: "usr_owner",
        displayName: "Станислав Лепехов",
        emailHint: "s***@example.test",
      },
      platformRole: "OWNER",
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: {
            app: { currentUser: "{name}, роль: {role}" },
            access: { roles: { OWNER: "Владелец" } },
          },
        },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Станислав Лепехов");
    expect(html).toContain("Владелец");
    expect(html).toContain('aria-haspopup="menu"');
    expect(html).toContain('aria-expanded="false"');
    expect(html).not.toContain("usr_owner");
    expect(html).not.toContain("example.test");
  });
});
