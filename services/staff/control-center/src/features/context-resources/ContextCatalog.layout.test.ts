import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./ContextCatalog.vue", import.meta.url),
  "utf8",
);

describe("каталог контекстных ресурсов", () => {
  it("не предлагает полноэкранный режим для короткого списка", () => {
    expect(source).toContain('v-if="!expanded && (total > 6 || cursor)"');
  });
});
