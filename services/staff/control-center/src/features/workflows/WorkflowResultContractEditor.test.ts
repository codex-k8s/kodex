import { renderToString } from "@vue/server-renderer";
import { createSSRApp } from "vue";
import { expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import Editor from "./WorkflowResultContractEditor.vue";

it.each(["ru", "en"] as const)(
  "показывает условия PR и сохранённую потерянную зависимость (%s)",
  async (locale) => {
    const previous = i18n.global.locale.value;
    i18n.global.locale.value = locale;
    const props = {
      stepKey: "review",
      dependencies: ["code", "renamed-stage"],
      requirements: [
        { key: "review", kind: "REVIEW", subjectStepKey: "code" },
        { key: "pr", kind: "PULL_REQUEST", repository: "codex-k8s/test-repo" },
      ],
      predecessors: [{ key: "code", name: "Разработка" }],
      disabled: true,
    };
    const before = structuredClone(props);
    try {
      const html = await renderToString(createSSRApp(Editor, props).use(i18n));
      expect(html).toContain("codex-k8s/test-repo");
      expect(html).toContain("renamed-stage");
      expect(html).toContain("Разработка");
      expect(html).toContain("<fieldset disabled");
      expect(html).not.toContain("workflowResults.");
      expect(props).toEqual(before);
      const ids = [...html.matchAll(/ id="([^"]+)"/gu)].map(
        (match) => match[1],
      );
      expect(ids.length).toBeGreaterThan(4);
      expect(new Set(ids).size).toBe(ids.length);
    } finally {
      i18n.global.locale.value = previous;
    }
  },
);


it("сохраняет exact ARTIFACT_REVIEW subject и показывает потерянный published result без подмены", async () => {
  const props = { stepKey: "review", dependencies: ["author"], requirements: [{ key: "verdict", kind: "ARTIFACT_REVIEW", subjectStepKey: "author", subjectResultKey: "removed-output" }], predecessors: [{ key: "author", name: "Автор", artifactKeys: ["current-output"] }], disabled: true };
  const before = structuredClone(props);
  const html = await renderToString(createSSRApp(Editor, props).use(i18n));
  expect(html).toContain("removed-output"); expect(html).toContain("current-output"); expect(html).toContain("ARTIFACT_REVIEW");
  expect(html).not.toContain("workflowResults."); expect(props).toEqual(before);
});
