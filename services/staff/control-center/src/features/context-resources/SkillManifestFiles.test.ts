import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it } from "vitest";
import SkillManifestFiles from "./SkillManifestFiles.vue";

describe("Skill manifest files", () => {
  it.each([false, true])(
    "ограничивает список шестью строками и сохраняет раскрытие при disabled=%s",
    async (disabled) => {
      const app = createSSRApp({
        render: () =>
          h(SkillManifestFiles, {
            disabled,
            modelValue: Array.from({ length: 8 }, (_, index) => ({
              path: index === 0 ? "SKILL.md" : `file-${String(index)}.md`,
              artifactRef: `artifact_${String(index)}`,
              artifactRevision: 3,
            })),
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
      expect(html.match(/class="context-file"/g)).toHaveLength(6);
      expect(html).toContain("artifact_5 / r3");
      expect(html).not.toContain("artifact_6");
      expect(html).toMatch(
        /<button[^>]*aria-label="managed.expandFields"[^>]*>/,
      );
      const expand = html.match(
        /<button[^>]*aria-label="managed.expandFields"[^>]*>/,
      )?.[0];
      expect(expand).not.toContain("disabled");
      expect(html.match(/<input[^>]*disabled/g)?.length ?? 0).toBe(
        disabled ? 6 : 0,
      );
    },
  );
});
