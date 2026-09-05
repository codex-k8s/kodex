import { afterEach, describe, expect, it, vi } from "vitest";
import {
  consumeEnvironmentDraftReference,
  environmentDraftReauthKey,
  rememberEnvironmentDraft,
} from "./environment-draft-reauth";
import type { RuntimeEnvironmentDraft } from "@/shared/api/generated/openapi/types.gen";
function storage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    key: (index) => [...map.keys()][index] ?? null,
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
    removeItem: (key) => {
      map.delete(key);
    },
    clear: () => map.clear(),
  };
}
const draft: RuntimeEnvironmentDraft = {
  ref: "draft_synthetic",
  version: 1,
  projectRef: "project_synthetic",
  expectedEnvironmentVersion: 0,
  state: "VALID",
  validationDigest: "a".repeat(64),
  diagnostics: [],
  specification: {
    name: "",
    description: "",
    imageArtifactRef: "",
    tools: [],
    values: [{ name: "TEST", value: "not-for-browser-storage" }],
    secretBindings: [],
  },
};
describe("возврат к серверному черновику после OIDC", () => {
  afterEach(() => vi.useRealTimers());
  it("хранит только reference и выдаёт его однократно в правильном scope", () => {
    const target = storage();
    rememberEnvironmentDraft(draft, target);
    expect(target.getItem(environmentDraftReauthKey)).not.toContain(
      "not-for-browser-storage",
    );
    expect(target.getItem(environmentDraftReauthKey)).not.toContain(
      "specification",
    );
    expect(
      consumeEnvironmentDraftReference(draft.projectRef, undefined, target),
    ).toBe(draft.ref);
    expect(
      consumeEnvironmentDraftReference(draft.projectRef, undefined, target),
    ).toBeUndefined();
  });
  it("отклоняет чужой проект, окружение и истёкший reference", () => {
    const target = storage();
    rememberEnvironmentDraft(draft, target);
    expect(() =>
      consumeEnvironmentDraftReference("project_other", undefined, target),
    ).toThrow();
    expect(target.length).toBe(0);
    rememberEnvironmentDraft(draft, target);
    expect(() =>
      consumeEnvironmentDraftReference(
        draft.projectRef,
        "environment_other",
        target,
      ),
    ).toThrow();
    vi.useFakeTimers();
    rememberEnvironmentDraft(draft, target);
    vi.advanceTimersByTime(300_001);
    expect(() =>
      consumeEnvironmentDraftReference(draft.projectRef, undefined, target),
    ).toThrow();
  });
});
