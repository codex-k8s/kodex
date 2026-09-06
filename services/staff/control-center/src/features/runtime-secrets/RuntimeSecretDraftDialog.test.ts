import { beforeEach, expect, it, vi } from "vitest";
import type { Ref } from "vue";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { RuntimeSecretDraft } from "./draft-api";

const api = vi.hoisted(() => ({
  createSecretDraft: vi.fn(),
  saveSecretDraft: vi.fn(),
  readSecretDraft: vi.fn(),
  changeSecretDraft: vi.fn(),
  safeDraftProblem: vi.fn(() => ({ code: "UNAVAILABLE" })),
}));
vi.mock("./draft-api", () => api);
vi.mock("@/shared/api/mutation", () => ({
  idempotencyKey: () => "original-key",
}));
vi.mock("@/shared/ui/unsaved-changes", () => ({ useUnsavedChanges: vi.fn() }));
import RuntimeSecretDraftDialog from "./RuntimeSecretDraftDialog.vue";

const draft = {
  ref: "draft_1",
  version: 2,
  projectRef: "project_1",
  secretRef: "secret_1",
  secretVersion: 1,
  state: "DRAFT",
} as RuntimeSecretDraft;
async function dialog() {
  return (await captureSetupState(
    RuntimeSecretDraftDialog,
    (app) => app.use(createI18n({ legacy: false, locale: "ru" })),
    { projectRef: "project_1" },
  )) as unknown as {
    draft: Ref<RuntimeSecretDraft | undefined>;
    uncertain: Ref<boolean>;
    busy: Ref<boolean>;
    impactBusy: Ref<boolean>;
    save(input: {
      name: string;
      description: string;
      valueType: "STRING";
      value: string;
    }): Promise<void>;
    change(action: "validate" | "discard"): Promise<void>;
    refresh(): Promise<void>;
  };
}
beforeEach(() => vi.clearAllMocks());

it("завершение metadata read не разблокирует команды во время публикации", async () => {
  const state = await dialog();
  state.draft.value = draft;
  state.busy.value = true;
  state.impactBusy.value = true;
  state.busy.value = false;
  await state.refresh();
  await state.change("discard");
  expect(api.readSecretDraft).not.toHaveBeenCalled();
  expect(api.changeSecretDraft).not.toHaveBeenCalled();
});

it("после потери ACK повторяет исходные данные и удаляет их после receipt", async () => {
  const state = await dialog();
  api.createSecretDraft
    .mockRejectedValueOnce(new Error("lost ACK"))
    .mockResolvedValueOnce(draft);
  const input = {
    name: "TOKEN",
    description: "",
    valueType: "STRING" as const,
    value: "original",
  };
  await state.save(input);
  expect(state.uncertain.value).toBe(true);
  expect(state.draft.value).toBeUndefined();
  const firstInput = api.createSecretDraft.mock.calls[0]?.[1] as {
    value: string;
  };
  expect(firstInput.value).toBe("original");
  await state.save({ ...input, value: "changed" });
  expect(api.createSecretDraft.mock.calls[1]?.[1]).toBe(firstInput);
  expect(api.createSecretDraft.mock.calls[1]?.[2]).toBe("original-key");
  expect(firstInput.value).toBe("");
  expect(state.draft.value).toEqual(draft);
  expect(state.uncertain.value).toBe(false);
});

it("не запускает второй save пока первый ожидает ответ", async () => {
  const state = await dialog();
  let finish!: (value: RuntimeSecretDraft) => void;
  api.createSecretDraft.mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  const input = {
    name: "TOKEN",
    description: "",
    valueType: "STRING" as const,
    value: "original",
  };
  const saving = state.save(input);
  await state.save(input);
  expect(api.createSecretDraft).toHaveBeenCalledOnce();
  finish(draft);
  await saving;
});

it("новая команда читает authoritative draft, retry сохраняет прежний OCC", async () => {
  const state = await dialog();
  state.draft.value = draft;
  const current = { ...draft, version: 5, secretVersion: 7 };
  api.readSecretDraft.mockResolvedValue(current);
  api.changeSecretDraft
    .mockRejectedValueOnce(new Error("lost ACK"))
    .mockResolvedValueOnce({ ...current, state: "VALID" });
  await state.change("validate");
  expect(api.changeSecretDraft).toHaveBeenLastCalledWith(
    current,
    "validate",
    "original-key",
  );
  await state.change("discard");
  expect(api.changeSecretDraft).toHaveBeenCalledTimes(1);
  await state.change("validate");
  expect(api.readSecretDraft).toHaveBeenCalledOnce();
  expect(api.changeSecretDraft.mock.calls[0]).toEqual(
    api.changeSecretDraft.mock.calls[1],
  );
  expect(state.uncertain.value).toBe(false);
});
