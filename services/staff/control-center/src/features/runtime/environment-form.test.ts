import { describe, expect, it } from "vitest";

import {
  defaultRuntimeEnvironmentPolicy,
  editableRuntimeEnvironmentPolicy,
  editableSecretBindings,
  emptySecretBinding,
  normalizeRuntimeEnvironmentInput,
  runtimeEnvironmentCollectionLimit,
  setRuntimeKubernetesAccess,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";

describe("runtime environment form", () => {
  it("сохранение иных полей сохраняет точный Secret pin и отклоняет повреждённую revision", () => {
    const input = {
      name: "Changed",
      description: "",
      imageArtifactRef: "image",
      tools: [],
      values: [],
      secretBindings: [{ name: "TOKEN", secretRef: "secret", revision: 7 }],
      policy: defaultRuntimeEnvironmentPolicy(),
    };
    expect(normalizeRuntimeEnvironmentInput(input).secretBindings).toEqual(
      input.secretBindings,
    );
    input.secretBindings = [
      { name: "TOKEN", secretRef: "secret", revision: -1 },
    ];
    expect(
      validateEnvironmentInput(input).map((problem) => problem.message),
    ).toContain("runtime.errors.secretRevision");
  });
  it("принимает несекретные значения и ссылку на runtime secret", () => {
    expect(
      validateEnvironmentInput({
        name: "Документы",
        description: "Работа с документами",
        imageArtifactRef: "imgart_documents",
        tools: [
          {
            name: "GitHub CLI",
            command: "gh",
            description: "Работа с разрешёнными репозиториями",
            usageHint: "Используйте gh для операций с GitHub.",
          },
        ],
        values: [{ name: "OUTPUT_FORMAT", value: "markdown" }],
        secretBindings: [
          {
            name: "PROVIDER_TOKEN",
            secretRef: "secret_provider_token",
          },
        ],
        policy: defaultRuntimeEnvironmentPolicy(),
      }),
    ).toEqual([]);
  });

  it("закрыто отклоняет повторы, небезопасные имена и пустую ссылку", () => {
    const binding = emptySecretBinding();
    binding.name = "bad-name";
    const problems = validateEnvironmentInput({
      name: " ",
      description: "",
      imageArtifactRef: "",
      tools: [
        {
          name: " ",
          command: "bad command",
          description: " ",
          usageHint: "",
        },
        {
          name: "Повтор",
          command: "bad command",
          description: "Описание",
          usageHint: "",
        },
      ],
      values: [
        { name: "DUPLICATE", value: "one" },
        { name: "DUPLICATE", value: "two" },
        { name: "KODEX_INTERNAL", value: "forbidden" },
      ],
      secretBindings: [binding],
      policy: defaultRuntimeEnvironmentPolicy(),
    });

    expect(problems.map((item) => item.message)).toEqual(
      expect.arrayContaining([
        "runtime.errors.nameRequired",
        "runtime.errors.imageRequired",
        "runtime.errors.toolNameRequired",
        "runtime.errors.toolCommand",
        "runtime.errors.toolDescriptionRequired",
        "runtime.errors.duplicateTool",
        "runtime.errors.duplicateVariable",
        "runtime.errors.variableName",
        "runtime.errors.reservedVariableName",
        "runtime.errors.secretBindingRequired",
      ]),
    );
  });

  it("не переносит server-generated descriptor в редактируемый input", () => {
    expect(
      editableSecretBindings([
        {
          name: "PROVIDER_TOKEN",
          secretRef: "secret_provider_token",
          secretName: "runtime-provider-token",
          secretKey: "value",
          secretUid: "4ea063ab-b3ee-49fd-b6d2-d0f44fd85bb1",
          secretResourceVersion: "128",
          revision: 7,
          contentSha256: "a".repeat(64),
        },
      ]),
    ).toEqual([
      {
        name: "PROVIDER_TOKEN",
        secretRef: "secret_provider_token",
        revision: 7,
      },
    ]);
  });

  it("создаёт безопасную policy по умолчанию и связывает Kubernetes API с scoped RBAC", () => {
    const policy = defaultRuntimeEnvironmentPolicy();

    expect(policy).toEqual({
      resources: {
        cpuRequestMilli: 2000,
        cpuLimitMilli: 2000,
        memoryRequestMib: 4096,
        memoryLimitMib: 4096,
        ephemeralStorageRequestMib: 1024,
        ephemeralStorageLimitMib: 4096,
      },
      volumes: [],
      networkDestinations: ["DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK"],
      kubernetesAccess: "NONE",
    });

    setRuntimeKubernetesAccess(policy, "READ_OWN_EXECUTION");
    expect(policy.networkDestinations).toEqual([
      "DNS",
      "PROVIDER_PROXY",
      "RUNTIME_CALLBACK",
      "KUBERNETES_API",
    ]);
    setRuntimeKubernetesAccess(policy, "NONE");
    expect(policy.networkDestinations).not.toContain("KUBERNETES_API");
  });

  it("преобразует effective policy только в редактируемые поля", () => {
    expect(
      editableRuntimeEnvironmentPolicy({
        resources: defaultRuntimeEnvironmentPolicy().resources,
        volumes: [
          {
            name: "workspace-cache",
            kind: "EPHEMERAL_DISK",
            sizeMib: 2048,
            mountPath: "/workspace/.kodex/volumes/workspace-cache",
          },
        ],
        network: {
          denyByDefault: true,
          egress: [
            { destination: "DNS", protocol: "TCP", port: 53 },
            { destination: "DNS", protocol: "UDP", port: 53 },
            { destination: "PROVIDER_PROXY", protocol: "TCP", port: 8080 },
            { destination: "RUNTIME_CALLBACK", protocol: "TCP", port: 8444 },
            { destination: "KUBERNETES_API", protocol: "TCP", port: 443 },
          ],
        },
        kubernetesAccess: {
          kind: "READ_OWN_EXECUTION",
          namespace: "kodex-runtime",
        },
        resourcesDigest: "a".repeat(64),
        volumesDigest: "b".repeat(64),
        networkDigest: "c".repeat(64),
        rbacDigest: "d".repeat(64),
      }),
    ).toEqual({
      resources: defaultRuntimeEnvironmentPolicy().resources,
      volumes: [
        {
          name: "workspace-cache",
          kind: "EPHEMERAL_DISK",
          sizeMib: 2048,
        },
      ],
      networkDestinations: [
        "DNS",
        "PROVIDER_PROXY",
        "RUNTIME_CALLBACK",
        "KUBERNETES_API",
      ],
      kubernetesAccess: "READ_OWN_EXECUTION",
    });
  });

  it("закрыто отклоняет policy вне admission ranges и несогласованную сеть", () => {
    const policy = defaultRuntimeEnvironmentPolicy();
    policy.resources.cpuRequestMilli = 99;
    policy.resources.cpuLimitMilli = 50;
    policy.resources.memoryLimitMib = 64;
    policy.resources.ephemeralStorageLimitMib = 128;
    policy.volumes = [
      { name: "tmp", kind: "EPHEMERAL_MEMORY", sizeMib: 8 },
      { name: "tmp", kind: "EPHEMERAL_DISK", sizeMib: 1024 },
    ];
    policy.networkDestinations.push("KUBERNETES_API");

    const problems = validateEnvironmentInput({
      name: "Окружение",
      description: "",
      imageArtifactRef: "imgart_main",
      tools: [],
      values: [],
      secretBindings: [],
      policy,
    });

    expect(problems.map((item) => item.message)).toEqual(
      expect.arrayContaining([
        "runtime.errors.cpuRequestRange",
        "runtime.errors.cpuLimitRange",
        "runtime.errors.cpuLimitBelowRequest",
        "runtime.errors.memoryLimitRange",
        "runtime.errors.memoryLimitBelowRequest",
        "runtime.errors.ephemeralStorageLimitRange",
        "runtime.errors.ephemeralStorageLimitBelowRequest",
        "runtime.errors.reservedVolumeName",
        "runtime.errors.duplicateVolume",
        "runtime.errors.volumeSizeRange",
        "runtime.errors.networkDestinations",
      ]),
    );
  });

  it("фиксирует единый ограниченный размер редактируемых коллекций", () => {
    expect(runtimeEnvironmentCollectionLimit).toBe(128);
  });
});
