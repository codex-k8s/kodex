import type { EmailMailboxConfigurationView } from "@/shared/api/generated/openapi/types.gen";

// Создаёт независимые synthetic configuration и revision для тестов почтового ящика.
export function mailboxConfigurationFixture(): Pick<
  EmailMailboxConfigurationView,
  "configuration" | "revision"
> {
  return {
    configuration: {
      ref: "configuration",
      version: 3,
      kind: "EMAIL_MAILBOX",
      name: "Mail",
      managedBy: "UI",
      archived: false,
      nextActions: [],
      source: "",
      sourceRevision: "",
      updatedAt: "2026-09-05T00:00:00Z",
    },
    revision: {
      ref: "revision",
      revision: 2,
      state: "DRAFT",
      contentFormat: "YAML",
      content: "enabled: false\n",
      digest: "a".repeat(64),
      validationDiagnostics: [],
      createdAt: "2026-09-05T00:00:00Z",
    },
  };
}
