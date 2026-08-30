import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./HomeAttentionCenter.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("HomeAttentionCenter states", () => {
  it("разделяет решения и аварийные запуски", () => {
    expect(template).toContain('v-if="gates.length"');
    expect(template).toContain('v-if="failedRuns.length"');
    expect(template).toContain("gate.contextSummary");
    expect(template).toContain("run.safeErrorMessage");
  });

  it("имеет отдельные loading, error и empty состояния", () => {
    expect(template).toContain('v-if="initialLoading"');
    expect(template).toContain('v-if="gatesProblem"');
    expect(template).toContain('v-if="runsProblem"');
    expect(template).toContain("total === 0");
  });

  it("не обещает недоступные provider/account данные", () => {
    expect(template).not.toMatch(/provider|account/i);
  });
});
