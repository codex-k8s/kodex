import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it } from "vitest";

import RoleImageDockerfileEditor from "@/features/role-images/RoleImageDockerfileEditor.vue";

describe("RoleImageDockerfileEditor", () => {
  it("рендерит контейнер CodeMirror и не подменяет source", async () => {
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

    expect(html).toContain('class="dockerfile-editor__viewport"');
    expect(html).not.toContain("FROM ubuntu:24.04");
    expect(html).toContain(`3 · ${String(source.length)}`);
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

    expect(html).toContain("is-readonly");
    expect(html).toContain("Нужен FROM");
  });
});
