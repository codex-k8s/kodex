import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import ViewModeToggle from "@/shared/ui/ViewModeToggle.vue";
import { viewModeFromNavigationKey } from "@/shared/ui/view-mode-toggle";

describe("viewModeFromNavigationKey", () => {
  it("поддерживает стрелки, Home и End", () => {
    expect(viewModeFromNavigationKey("list", "ArrowRight")).toBe("grid");
    expect(viewModeFromNavigationKey("grid", "ArrowDown")).toBe("list");
    expect(viewModeFromNavigationKey("grid", "Home")).toBe("list");
    expect(viewModeFromNavigationKey("list", "End")).toBe("grid");
    expect(viewModeFromNavigationKey("list", "Enter")).toBeUndefined();
  });
});

describe("ViewModeToggle", () => {
  it("передаёт экранному диктору группу и выбранный режим", async () => {
    const app = createSSRApp({
      render: () =>
        h(ViewModeToggle, {
          ariaLabel: "Вид результатов",
          gridLabel: "Сетка",
          listLabel: "Список",
          modelValue: "grid",
        }),
    });

    const html = await renderToString(app);

    expect(html).toContain('role="group"');
    expect(html).toContain('aria-label="Вид результатов"');
    expect(html).toContain('aria-label="Сетка"');
    expect(html).toContain('aria-pressed="true"');
  });
});
