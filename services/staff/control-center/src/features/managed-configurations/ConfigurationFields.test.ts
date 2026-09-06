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
    expect(html).toContain("ru\nen");
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
