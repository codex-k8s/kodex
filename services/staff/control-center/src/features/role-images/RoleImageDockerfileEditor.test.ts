import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it } from "vitest";

import RoleImageDockerfileEditor from "@/features/role-images/RoleImageDockerfileEditor.vue";

describe("RoleImageDockerfileEditor", () => {
  it("рендерит строки и синтаксические токены без преобразования source", async () => {
    const source = "FROM ubuntu:24.04\nRUN echo ${HOME}\n# comment";
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(RoleImageDockerfileEditor, {
            label: "Dockerfile",
            modelValue: source,
          }),
      }),
    );

    expect(html).toContain('class="token--instruction"');
    expect(html).toContain('class="token--variable"');
    expect(html).toContain('class="token--comment"');
    expect(html).toContain("FROM ubuntu:24.04");
    expect(html).toContain("${HOME}");
    expect(html).toContain(`3 · ${source.length}`);
  });

  it("делает исходник readonly и показывает validation boundary", async () => {
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(RoleImageDockerfileEditor, {
            label: "Dockerfile",
            modelValue: "",
            readonly: true,
            validationMessages: ["Нужен FROM"],
          }),
      }),
    );

    expect(html).toContain("readonly");
    expect(html).toContain("Нужен FROM");
    expect(html).toContain('aria-invalid="true"');
  });
});
