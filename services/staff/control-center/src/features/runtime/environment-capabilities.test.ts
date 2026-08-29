import { describe, expect, it } from "vitest";

import {
  compactIdentifier,
  environmentReadiness,
  runtimeEnvironmentCapabilities,
  safeSecretReference,
} from "@/features/runtime/environment-capabilities";

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
      resources: "UNAVAILABLE",
      networkPolicy: "UNAVAILABLE",
      kubernetesRbac: "UNAVAILABLE",
      effectivePolicy: "UNAVAILABLE",
      secretLifecycle: "UNAVAILABLE",
      secretReveal: "UNAVAILABLE",
      serverReadiness: "UNAVAILABLE",
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
    };
    const checks = environmentReadiness(input, {
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
        digest: "a".repeat(64),
        createdAt: "2026-08-29T12:00:00Z",
      },
      updatedAt: "2026-08-29T12:00:00Z",
    });

    expect(checks.map(({ key, state }) => ({ key, state }))).toEqual([
      { key: "FORM", state: "READY" },
      { key: "SECRET_REFS", state: "READY" },
      { key: "IMAGE", state: "READY" },
      { key: "TOOLS", state: "READY" },
      { key: "REVISION", state: "READY" },
      { key: "SERVER_READINESS", state: "UNAVAILABLE" },
    ]);
  });

  it("строит только безопасное представление ссылки на секрет", () => {
    const reference = safeSecretReference({
      name: "PROVIDER_TOKEN",
      secretRef: "secret_provider_token",
      secretName: "provider-token",
      secretKey: "token",
      secretUid: "4ea063ab-b3ee-49fd-b6d2-d0f44fd85bb1",
      secretResourceVersion: "128",
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
