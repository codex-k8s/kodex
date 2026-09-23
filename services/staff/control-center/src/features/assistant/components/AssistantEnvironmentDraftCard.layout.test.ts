import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantEnvironmentDraftCard.vue", import.meta.url),
  "utf8",
);

describe("AssistantEnvironmentDraftCard", () => {
  it("читает только черновик из точной квитанции и не считает его опубликованным", () => {
    expect(source).toContain("assistantEnvironmentDraftTarget");
    expect(source).toContain("readEnvironmentDraft");
    expect(source).toContain("draft.state === 'PUBLISHED'");
    expect(source).toContain("environmentDraft.incomplete");
  });

  it("открывает существующий редактор с exact draftRef", () => {
    expect(source).toContain('name: "runtime-environment-new"');
    expect(source).toContain("query: { draftRef: exact.draftRef }");
    expect(source).toContain("emit('navigate')");
  });
});
