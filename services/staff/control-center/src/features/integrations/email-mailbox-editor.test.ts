import { beforeEach, expect, it, vi } from "vitest";
import type { EmailMailboxConfigurationView } from "@/shared/api/generated/openapi/types.gen";
const api = vi.hoisted(() => ({
  readMailbox: vi.fn(),
  listMailboxes: vi.fn(),
  previewMailbox: vi.fn(),
  createMailboxDraft: vi.fn(),
  saveMailboxDraft: vi.fn(),
  changeMailboxDraft: vi.fn(),
  bindMailbox: vi.fn(),
  unbindMailbox: vi.fn(),
}));
vi.mock("./email-mailbox-api", () => api);
vi.mock("@/shared/api/mutation", () => ({
  idempotencyKey: () => "original-key",
}));
import { mailboxEditor } from "./email-mailbox-editor";

function view(enabled = true): EmailMailboxConfigurationView {
  const action = (
    action: EmailMailboxConfigurationView["nextActions"][number]["action"],
  ) => ({
    action,
    enabled,
    reason: enabled ? ("NONE" as const) : ("STATE" as const),
  });
  return {
    connectionRef: "connection",
    connectionVersion: 8,
    mailboxRef: "mailbox",
    configuration: {
      ref: "configuration",
      version: 3,
      kind: "EMAIL_MAILBOX",
      name: "Mail",
      managedBy: "UI",
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
    specification: { enabled: false },
    diagnostics: [],
    boundRevisionRef: "",
    nextActions: [
      action("CREATE_DRAFT"),
      action("SAVE"),
      action("VALIDATE"),
      action("PUBLISH"),
      action("DISCARD"),
      action("BIND"),
      action("UNBIND"),
      action("DETACH"),
      action("COPY"),
    ],
  };
}
beforeEach(() => {
  vi.resetAllMocks();
  api.readMailbox.mockResolvedValue(view());
});
it("не выводит write authority из UI/DRAFT", async () => {
  api.readMailbox.mockResolvedValue(view(false));
  const editor = mailboxEditor("connection");
  await editor.open();
  expect(editor.writable.value).toBe(false);
  await editor.execute("SAVE");
  expect(api.saveMailboxDraft).not.toHaveBeenCalled();
});
it("не перебазирует несохранённое содержимое поверх изменившейся owner version", async () => {
  const editor = mailboxEditor("connection");
  await editor.open();
  editor.specification.value = { enabled: true };
  const fresh = view();
  fresh.connectionVersion++;
  api.readMailbox.mockResolvedValue(fresh);
  await editor.execute("SAVE");
  expect(editor.problem.value?.status).toBe(412);
  expect(editor.specification.value).toEqual({ enabled: true });
  expect(api.saveMailboxDraft).not.toHaveBeenCalled();
});
it("после неизвестного ACK повторяет исходный snapshot/key/OCC, независимо от нового ввода", async () => {
  const editor = mailboxEditor("connection");
  await editor.open();
  editor.specification.value = { enabled: true };
  api.saveMailboxDraft
    .mockRejectedValueOnce(new Error("lost ACK"))
    .mockResolvedValueOnce(view());
  await editor.execute("SAVE");
  expect(editor.uncertain.value).toBe(true);
  const first: unknown = api.saveMailboxDraft.mock.calls[0];
  editor.specification.value = { enabled: false, sender: "changed" };
  await editor.execute();
  expect(api.saveMailboxDraft.mock.calls[1]).toEqual(first);
  expect(editor.uncertain.value).toBe(false);
  expect(api.readMailbox).toHaveBeenCalledTimes(2);
});
it("parse failure сохраняет YAML и не открывает восстановленную форму", async () => {
  const editor = mailboxEditor("connection");
  await editor.open();
  editor.mode.value = "YAML";
  editor.yaml.value = "broken: [";
  api.previewMailbox.mockResolvedValue({
    canonicalYaml: "",
    valid: false,
    diagnostics: [{ code: "EMAIL_MAILBOX_SYNTAX_INVALID" }],
  });
  await editor.preview("FORM");
  expect(editor.mode.value).toBe("YAML");
  expect(editor.yaml.value).toBe("broken: [");
  expect(editor.diagnostics.value).toHaveLength(1);
});
it("закрытый редактор не принимает поздний mutation ACK", async () => {
  const editor = mailboxEditor("connection");
  await editor.open();
  let finish: (value: EmailMailboxConfigurationView) => void = () => {
    throw new Error("Mutation not started");
  };
  api.saveMailboxDraft.mockImplementation(
    () =>
      new Promise<EmailMailboxConfigurationView>((resolve) => {
        finish = resolve;
      }),
  );
  const work = editor.execute("SAVE");
  await vi.waitFor(() => expect(api.saveMailboxDraft).toHaveBeenCalledOnce());
  editor.dispose();
  const late = view();
  late.configuration.version = 9;
  finish(late);
  await work;
  expect(editor.view.value?.configuration.version).toBe(3);
});
