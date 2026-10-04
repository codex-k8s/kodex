import { initializeRuntimeOwnerFixture } from "@/test-utils/runtime-owner-fixture";
import { beforeEach, describe, expect, it } from "vitest";
import { editableAssistantEnvironment } from "./environment-settings";
import { defaultRuntimeEnvironmentPolicy } from "@/features/runtime/environment-form";
import type { RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";

function environment(projectRef?: string): RuntimeEnvironmentSet {
  const policy = defaultRuntimeEnvironmentPolicy();
  return {
    scopeKind: projectRef ? "PROJECT" : "ORGANIZATION",
    organizationRef: "org_alpha",
    ref: "env_assistant",
    version: 1,
    projectRef: projectRef ?? "",
    name: "Окружение помощника",
    description: "Настроенное окружение",
    currentVersion: {
      ref: "envrev_assistant",
      version: 1,
      revision: 1,
      image: {
        artifactRef: "imgart_exact",
        recipeRef: "imgrecipe_assistant",
        recipeGeneration: 3,
        reference: "example.invalid/assistant@sha256:fixture",
        digest: "a".repeat(64),
      },
      tools: [
        {
          name: "Git",
          command: "git",
          description: "Чтение репозитория",
          usageHint: "status",
        },
      ],
      values: [{ name: "APP_MODE", value: "test" }],
      secretDescriptors: [
        {
          name: "APP_TOKEN",
          secretRef: "secret_exact",
          revision: 4,
          secretName: "fixture",
          secretKey: "token",
          secretUid: "fixture",
          secretResourceVersion: "4",
          contentSha256: "b".repeat(64),
        },
      ],
      policy: {
        resources: policy.resources,
        volumes: [],
        network: {
          denyByDefault: true,
          egress: [
            { destination: "DNS", protocol: "TCP", port: 53 },
            { destination: "DNS", protocol: "UDP", port: 53 },
            { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8084 },
            { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
          ],
          webAccess: { mode: "NONE", rules: [] },
        },
        kubernetesAccess: { kind: "NONE", namespace: "kodex-runtime" },
        resourcesDigest: "c".repeat(64),
        volumesDigest: "d".repeat(64),
        networkDigest: "e".repeat(64),
        rbacDigest: "f".repeat(64),
      },
      digest: "a".repeat(64),
      createdAt: "2026-10-03T00:00:00Z",
    },
    state: "ACTIVE",
    ready: true,
    readinessBlockers: [],
    updatedAt: "2026-10-03T00:00:00Z",
    nextActions: ["UPDATE"],
  };
}

describe("Настройка окружения помощника", () => {
  it("сохраняет точный образ, инструменты и ревизии секретов без материала", () => {
    const current = environment();
    const input = editableAssistantEnvironment(current, {
      kind: "ORGANIZATION",
      organizationRef: "org_alpha",
    });
    expect(input.imageArtifactRef).toBe("imgart_exact");
    expect(input.tools).toEqual(current.currentVersion.tools);
    expect(input.secretBindings).toEqual([
      { name: "APP_TOKEN", secretRef: "secret_exact", revision: 4 },
    ]);
    expect(JSON.stringify(input)).not.toContain("secretUid");
    const tool = input.tools[0];
    const value = input.values[0];
    if (!tool || !value) throw new Error("Fixture fields are missing");
    tool.description = "Изменено";
    value.value = "changed";
    expect(current.currentVersion.tools[0]?.description).toBe(
      "Чтение репозитория",
    );
    expect(current.currentVersion.values[0]?.value).toBe("test");
  });

  it("не подставляет фиктивный artifactRef для отсутствующего образа", () => {
    const current = environment();
    current.currentVersion.image.artifactRef = "";
    expect(
      editableAssistantEnvironment(current, {
        kind: "ORGANIZATION",
        organizationRef: "org_alpha",
      }).imageArtifactRef,
    ).toBe("");
  });

  it("закрыто отклоняет смешивание организации и разных проектов", () => {
    expect(() =>
      editableAssistantEnvironment(environment("prj_alpha"), {
        kind: "ORGANIZATION",
        organizationRef: "org_alpha",
      }),
    ).toThrow("scope mismatch");
    expect(() =>
      editableAssistantEnvironment(environment(), {
        kind: "PROJECT",
        projectRef: "prj_alpha",
      }),
    ).toThrow("scope mismatch");
    expect(() =>
      editableAssistantEnvironment(environment("prj_beta"), {
        kind: "PROJECT",
        projectRef: "prj_alpha",
      }),
    ).toThrow("scope mismatch");
    expect(
      editableAssistantEnvironment(environment("prj_alpha"), {
        kind: "PROJECT",
        projectRef: "prj_alpha",
      }).imageArtifactRef,
    ).toBe("imgart_exact");
  });
});

beforeEach(() => initializeRuntimeOwnerFixture("org_alpha"));
