import { describe, expect, it } from "vitest";

import {
  emptySecretDescriptor,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";

describe("runtime environment form", () => {
  it("принимает несекретные значения и полный immutable Secret descriptor", () => {
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
        secretDescriptors: [
          {
            name: "PROVIDER_TOKEN",
            secretName: "runtime-provider",
            secretKey: "token",
            secretUid: "4ea063ab-b3ee-49fd-b6d2-d0f44fd85bb1",
            secretResourceVersion: "128",
            contentSha256: "a".repeat(64),
          },
        ],
      }),
    ).toEqual([]);
  });

  it("закрыто отклоняет повторы, небезопасные имена и неполный descriptor", () => {
    const descriptor = emptySecretDescriptor();
    descriptor.name = "bad-name";
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
      secretDescriptors: [descriptor],
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
        "runtime.errors.secretDescriptorRequired",
        "runtime.errors.sha256",
      ]),
    );
  });
});
