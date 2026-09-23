import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantEnvironmentDraftCard.vue", import.meta.url),
  "utf8",
);
const binding = readFileSync(
  new URL("./AssistantEnvironmentBindingDialog.vue", import.meta.url),
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

  it("после публикации предлагает защищённое назначение только сотруднику того же проекта", () => {
    expect(source).toContain("draft?.state === 'PUBLISHED'");
    expect(source).toContain(
      ':environment-ref="draft.publishedEnvironmentRef"',
    );
    expect(source).toContain(':project-ref="target.projectRef"');
    expect(binding).toContain("value.projectRef !== props.projectRef");
    expect(binding).toContain("route.params.projectRef !== props.projectRef");
    expect(binding).toContain("!value.ready");
    expect(binding).toContain("agent.projectRef !== props.projectRef");
    expect(binding).toContain('agent.nextActions.includes("EDIT")');
    expect(binding).toContain(
      "view.environment.projectRef !== props.projectRef",
    );
    expect(binding).toContain("view.agentVersion");
    expect(binding).toContain("bindRuntimeEnvironment(");
    expect(binding).toContain(
      "result.environmentBinding.agentRef !== agent.ref",
    );
  });
});
