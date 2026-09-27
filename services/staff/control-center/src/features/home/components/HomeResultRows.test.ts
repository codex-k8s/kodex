import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const template = readFileSync(
  new URL("./HomeResultRows.vue", import.meta.url),
  "utf8",
);

describe("HomeResultRows", () => {
  it("показывает краткое безопасное описание вместо полного результата запуска", () => {
    expect(template).toContain(
      '<SafeSummary :content="item.description" :maximum-length="140" />',
    );
    expect(template).not.toContain("{{ item.description }}");
  });
});
