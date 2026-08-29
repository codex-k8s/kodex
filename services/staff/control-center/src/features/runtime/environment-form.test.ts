import { describe, expect, it } from "vitest";

import {
  editableSecretBindings,
  emptySecretBinding,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";

describe("runtime environment form", () => {
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
          contentSha256: "a".repeat(64),
        },
      ]),
    ).toEqual([
      {
        name: "PROVIDER_TOKEN",
        secretRef: "secret_provider_token",
      },
    ]);
  });
});
