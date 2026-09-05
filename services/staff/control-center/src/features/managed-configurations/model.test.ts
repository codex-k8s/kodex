import { describe, expect, it } from "vitest";
import type {
  ManagedConfiguration,
  ManagedConfigurationConsumer,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import {
  canPublish,
  canValidate,
  consumerKey,
  selectedConsumers,
} from "./model";
const revision: ManagedConfigurationRevision = {
  ref: "revision_one",
  revision: 1,
  state: "DRAFT",
  contentFormat: "TEXT",
  content: "test",
  digest: "digest",
  validationDiagnostics: [],
  createdAt: "2026-09-04T00:00:00Z",
};
const configuration: ManagedConfiguration = {
  ref: "configuration_one",
  version: 1,
  kind: "PROMPT_TEMPLATE",
  name: "test",
  managedBy: "UI",
  source: "ui",
  sourceRevision: "1",
  updatedAt: revision.createdAt,
};
describe("managed configuration lifecycle", () => {
  it("публикация требует VALID, Git source не редактируется", () => {
    expect(canValidate(configuration, revision)).toBe(true);
    expect(canPublish(configuration, revision)).toBe(false);
    expect(canPublish(configuration, { ...revision, state: "VALID" })).toBe(
      true,
    );
    expect(
      canPublish(
        { ...configuration, managedBy: "GIT" },
        { ...revision, state: "VALID" },
      ),
    ).toBe(false);
    expect(
      canValidate(configuration, { ...revision, state: "PUBLISHED" }),
    ).toBe(false);
  });
  it("перепривязка сохраняет exact consumer revision/version из impact", () => {
    const consumer: ManagedConfigurationConsumer = {
      kind: "AGENT",
      ref: "agent_one",
      revisionRef: "revision_previous",
      version: 7,
    };
    expect(selectedConsumers([consumer], [consumerKey(consumer)])).toEqual([
      consumer,
    ]);
    expect(() => selectedConsumers([consumer], ["unknown"])).toThrow();
    expect(() =>
      selectedConsumers([consumer, consumer], [consumerKey(consumer)]),
    ).toThrow();
  });
});
