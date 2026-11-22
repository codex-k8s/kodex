import { createSSRApp } from "vue";
import { renderToString } from "vue/server-renderer";
import { createI18n } from "vue-i18n";
import { expect, it } from "vitest";
import { kubernetesGrantMessages } from "../kubernetes-grant-messages";
import Field from "./KubernetesGrantScopeField.vue";

it.each(["ru", "en"] as const)(
  "настоящий render %s сохраняет @ scope literals без linked-message parsing",
  async (locale) => {
    const app = createSSRApp(Field, { modelValue: "example" });
    app.use(
      createI18n({
        legacy: false,
        locale,
        messages: {
          ru: { kubernetesGrants: kubernetesGrantMessages.ru },
          en: { kubernetesGrants: kubernetesGrantMessages.en },
        },
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain(kubernetesGrantMessages[locale].namespaceLabel);
    expect(html).toContain("@cluster");
    expect(html).toContain("@all-namespaces");
    expect(html).toContain('value="example"');
  },
);
