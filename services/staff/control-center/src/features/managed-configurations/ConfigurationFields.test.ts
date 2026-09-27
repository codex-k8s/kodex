import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/ui/VoiceTextarea.vue", () => ({
  default: defineComponent({
    props: { modelValue: String, disabled: Boolean },
    setup: (props) => () =>
      h("textarea", { disabled: props.disabled }, props.modelValue),
  }),
}));
vi.mock("@/shared/ui/AsyncEntityPicker.vue", () => ({
  default: defineComponent({ setup: () => () => h("button", "Picker") }),
}));
vi.mock("@/features/role-images/RoleImageDockerfileEditor.vue", () => ({
  default: defineComponent({
    props: { modelValue: String },
    setup: (props) => () => h("pre", props.modelValue),
  }),
}));
import ConfigurationFields from "./ConfigurationFields.vue";
describe("STT configuration fields", () => {
  it("отображает полный сохраненный профиль без подмены eligibility", async () => {
    const app = createSSRApp({
      render: () =>
        h(ConfigurationFields, {
          kind: "SYSTEM_STT",
          name: "STT",
          format: "JSON",
          disabled: true,
          modelValue: JSON.stringify({
            stt: {
              enabled: true,
              model: "gpt-transcribe",
              language: "",
              parameters: {
                languages: ["ru", "en"],
                keywords: ["Kodex"],
                prompt: "Synthetic context",
                temperature: 0.4,
                chunkingStrategy: "auto",
                stream: false,
              },
              maximumAudioBytes: 10485760,
              maximumAudioDurationMilliseconds: 120000,
              providerTimeoutMilliseconds: 15000,
            },
          }),
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        missingWarn: false,
        messages: { ru: {} },
      }),
    );
    const html = await renderToString(app);
    expect(html).toMatch(/<select[^>]*-language"/);
    expect(html).toContain('value="custom"');
    expect(html).toMatch(/<select[^>]*-language"[^>]*value="ru"/);
    expect(html).toMatch(/-additional-languages"[^>]*>en<\/textarea>/);
    expect(html).toContain("configuration-fields__advanced");
    expect(html).toContain("Kodex");
    expect(html).toContain("Synthetic context");
    expect(html).toContain('value="0.4"');
    expect(html).toContain('value="120000"');
    expect(html).toContain('value="15000"');
    expect(html).toContain('value="10485760"');
    expect(html).toMatch(/<fieldset[^>]*disabled/);
    expect(html.match(/<textarea disabled/g)).toHaveLength(4);
    expect(html).not.toContain("speechTranscription");
  });
});

describe("Конфигурация образа роли", () => {
  it("показывает поля действующего рецепта, а не пустые поля старой схемы", async () => {
    const app = createSSRApp({
      render: () =>
        h(ConfigurationFields, {
          kind: "ROLE_IMAGE",
          name: "Образ координатора",
          format: "JSON",
          modelValue: JSON.stringify({
            name: "Образ координатора",
            roleImage: {
              roleDefinitionRef: "role_example",
              environment: {
                environmentKey: "standard",
                packageKeys: ["package.one"],
                toolKeys: ["tool.one"],
                installationBlock: "RUN echo safe",
                dockerfile: "FROM image@sha256:abc",
              },
            },
          }),
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        missingWarn: false,
        messages: { ru: {} },
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain('value="role_example"');
    expect(html).toContain('value="standard"');
    expect(html).toContain("package.one");
    expect(html).toContain("tool.one");
    expect(html).toContain("RUN echo safe");
    expect(html).toContain("FROM image@sha256:abc");
    expect(html).not.toMatch(/name="[^"]+-base-image"/);
  });
});
