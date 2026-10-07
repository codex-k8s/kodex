import { readFileSync } from "node:fs";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it } from "vitest";

import ArtifactList from "./ArtifactList.vue";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

const source = readFileSync(
  new URL("./ArtifactList.vue", import.meta.url),
  "utf8",
);

function artifact(index: number): Artifact {
  return {
    ref: `art_result_${String(index)}`,
    version: 1,
    projectRef: "prj_results",
    fileName:
      index === 19 ? "runtime-context.json" : `Отчёт ${String(index)}.md`,
    mediaType: "text/markdown",
    sizeBytes: 20,
    digest: "a".repeat(64),
    scanState: "CLEAN",
    source: "AGENT_RESULT",
    revision: 1,
    lifecycleState: "ACTIVE",
    agentBindings: [],
    previewAvailable: true,
    createdAt: "2026-10-07T04:00:00Z",
    nextActions: ["OPEN"],
  };
}

describe("ArtifactList bounded recent results", () => {
  it("ограничивает высоту примерно пятью строками и сохраняет клавиатурную прокрутку", () => {
    expect(source).toContain('role="region"');
    expect(source).toContain('tabindex="0"');
    expect(source).toContain(":aria-label=\"$t('workboard.recentResults')\"");
    expect(source).toContain("max-height: 300px");
    expect(source).toContain("max-height: 400px");
    expect(source).toContain("overflow-y: auto");
    expect(source).toContain(".artifact-list:focus-visible");
    expect(source).not.toMatch(/artifacts\.(?:slice|filter)\(/);
    expect(source).not.toMatch(
      /fetch\(|loadMore|nextPageToken|IntersectionObserver/,
    );
  });

  it("сохраняет все двадцать ссылок, включая служебные имена, без фиктивной пагинации", async () => {
    const artifacts = Array.from({ length: 20 }, (_, index) => artifact(index));
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/:pathMatch(.*)*", component: { render: () => null } }],
    });
    const app = createSSRApp({ render: () => h(ArtifactList, { artifacts }) });
    app.use(router);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        missingWarn: false,
        fallbackWarn: false,
        messages: {
          ru: {
            workboard: { recentResults: "Последние результаты" },
            files: { source: { AGENT_RESULT: "Результат сотрудника" } },
          },
        },
      }),
    );
    const html = await renderToString(app);
    expect(html.match(/class="artifact-item"/g)).toHaveLength(20);
    for (const item of artifacts) {
      expect(html).toContain(`artifactRef=${item.ref}`);
      expect(html).toContain(item.fileName);
    }
  });
});
