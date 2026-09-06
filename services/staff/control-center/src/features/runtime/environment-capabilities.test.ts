import { describe, expect, it } from "vitest";

import {
  compactIdentifier,
  environmentReadiness,
  hasEnvironmentAction,
  runtimeEnvironmentCapabilities,
  safeSecretReference,
} from "@/features/runtime/environment-capabilities";
import { defaultRuntimeEnvironmentPolicy } from "@/features/runtime/environment-form";
import type { RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";

function effectivePolicy() {
  return {
    resources: defaultRuntimeEnvironmentPolicy().resources,
    volumes: [],
    network: {
      denyByDefault: true as const,
      egress: [
        { destination: "DNS" as const, protocol: "TCP" as const, port: 53 },
        { destination: "DNS" as const, protocol: "UDP" as const, port: 53 },
        {
          destination: "PROVIDER_PROXY" as const,
          protocol: "TCP" as const,
          port: 8080,
        },
        {
          destination: "RUNTIME_CALLBACK" as const,
          protocol: "TCP" as const,
          port: 8444,
        },
      ],
    },
    kubernetesAccess: {
      kind: "NONE" as const,
      namespace: "kodex-runtime" as const,
    },
    resourcesDigest: "1".repeat(64),
    volumesDigest: "2".repeat(64),
    networkDigest: "3".repeat(64),
    rbacDigest: "4".repeat(64),
  };
}

describe("runtime environment capabilities", () => {
  it("не объявляет отсутствующие API доступными", () => {
    const matrix = Object.fromEntries(
      runtimeEnvironmentCapabilities.map((item) => [item.key, item.state]),
    );

    expect(matrix).toMatchObject({
      versioning: "AVAILABLE",
      search: "AVAILABLE",
      values: "AVAILABLE",
      secretReferences: "AVAILABLE",
      imageBinding: "AVAILABLE",
      verifiedTools: "AVAILABLE",
      resources: "AVAILABLE",
      networkPolicy: "AVAILABLE",
      kubernetesRbac: "AVAILABLE",
      effectivePolicy: "AVAILABLE",
      secretLifecycle: "AVAILABLE",
      secretReveal: "AVAILABLE",
      serverReadiness: "AVAILABLE",
    });
  });

  it("разделяет локальную валидацию, опубликованную ревизию и server readiness", () => {
    const input = {
      name: "Документы",
      description: "Безопасное окружение",
      imageArtifactRef: "imgart_documents",
      tools: [],
      values: [{ name: "OUTPUT_FORMAT", value: "markdown" }],
      secretBindings: [],
      policy: defaultRuntimeEnvironmentPolicy(),
    };
    const environment = {
      ref: "environment_docs",
      version: 3,
      projectRef: "project_main",
      name: input.name,
      description: input.description,
      state: "ACTIVE",
      currentVersion: {
        ref: "environment_version_docs",
        version: 3,
        revision: 3,
        values: input.values,
        secretDescriptors: [],
        image: {
          artifactRef: input.imageArtifactRef,
          recipeRef: "imgrec_documents",
          recipeGeneration: 1,
          reference: "registry.example/documents@sha256:" + "b".repeat(64),
          digest: "b".repeat(64),
        },
        tools: [],
        policy: effectivePolicy(),
        digest: "a".repeat(64),
        createdAt: "2026-08-29T12:00:00Z",
      },
      updatedAt: "2026-08-29T12:00:00Z",
      ready: true,
      readinessBlockers: [],
      nextActions: ["OPEN", "UPDATE", "DISABLE", "DELETE"],
    } satisfies RuntimeEnvironmentSet;
    const checks = environmentReadiness(input, environment, {
      environmentRef: environment.ref,
      environmentVersion: environment.version,
      publishedVersionRef: environment.currentVersion.ref,
      publishedVersionDigest: environment.currentVersion.digest,
      ready: true,
      blockers: [],
      observedAt: "2026-08-29T12:00:01Z",
    });

    expect(checks.map(({ key, state }) => ({ key, state }))).toEqual([
      { key: "FORM", state: "READY" },
      { key: "SECRET_REFS", state: "READY" },
      { key: "IMAGE", state: "READY" },
      { key: "TOOLS", state: "READY" },
      { key: "POLICY", state: "READY" },
      { key: "REVISION", state: "READY" },
      { key: "EFFECTIVE_POLICY", state: "READY" },
      { key: "SERVER_READINESS", state: "READY" },
    ]);
    expect(hasEnvironmentAction(environment, "DISABLE")).toBe(true);
    expect(hasEnvironmentAction(environment, "ENABLE")).toBe(false);
  });

  it("разделяет ошибку черновика policy и отсутствие effective policy", () => {
    const policy = defaultRuntimeEnvironmentPolicy();
    policy.resources.cpuLimitMilli = 100;

    const checks = environmentReadiness({
      name: "Документы",
      description: "",
      imageArtifactRef: "imgart_documents",
      tools: [],
      values: [],
      secretBindings: [],
      policy,
    });

    expect(checks).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          key: "POLICY",
          state: "NEEDS_ATTENTION",
        }),
        expect.objectContaining({
          key: "EFFECTIVE_POLICY",
          state: "NEEDS_ATTENTION",
        }),
      ]),
    );
  });

  it("строит только безопасное представление ссылки на секрет", () => {
    const reference = safeSecretReference({
      name: "PROVIDER_TOKEN",
      secretRef: "secret_provider_token",
      secretName: "provider-token",
      secretKey: "token",
      secretUid: "4ea063ab-b3ee-49fd-b6d2-d0f44fd85bb1",
      secretResourceVersion: "128",
      revision: 7,
      contentSha256: "b".repeat(64),
    });

    expect(reference).toEqual({
      name: "PROVIDER_TOKEN",
      target: "provider-token / token",
      revision: "128",
      uidHint: "4ea063ab…4fd85bb1",
      digestHint: "bbbbbbbb…bbbbbbbb",
    });
    expect(compactIdentifier("short")).toBe("short");
  });
});
