import { createPinia, setActivePinia } from "pinia";
import type { Ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, expect, it, vi } from "vitest";
import { createI18n } from "vue-i18n";
import { usePlatformStore } from "@/features/platform/store";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { OwnerGate, Run } from "@/shared/api/generated/openapi/types.gen";
import RunPage from "./RunPage.vue";

afterEach(() => vi.unstubAllGlobals());
async function page() {
  vi.stubGlobal("window", {
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    setTimeout,
    clearTimeout,
  });
  const pinia = createPinia();
  setActivePinia(pinia);
  const platform = usePlatformStore();
  const run = {
    ref: "run_one",
    rootRunRef: "run_one",
    projectRef: "project_one",
    sessionRef: "session_one",
    version: 1,
    state: "SUCCEEDED",
    nextActions: ["ADD_TURN"],
  } as Run;
  platform.runs[run.ref] = run;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/runs/:runRef", component: RunPage }],
  });
  await router.push("/runs/run_one");
  const send = vi.spyOn(platform, "continueSession").mockResolvedValue(run);
  const state = (await captureSetupState(RunPage, (app) => {
    app.use(pinia);
    app.use(router);
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        missingWarn: false,
        fallbackWarn: false,
      }),
    );
  })) as unknown as {
    turn: Ref<string>;
    busy: Ref<boolean>;
    turnAttachmentComposer: Ref<
      { finalize(): Promise<string>; clear(): void } | undefined
    >;
    continueRun(): Promise<void>;
    comments: Ref<Record<string, string>>;
    gateAttachmentComposers: Map<
      string,
      { finalize(): Promise<string>; clear(): void }
    >;
    decide(gate: OwnerGate, decision: "APPROVE"): Promise<void>;
  };
  return { state, router, send, platform };
}

it("фиксирует текст до ожидания вложений и отклоняет повторный submit", async () => {
  const { state, send } = await page();
  let finish!: (value: string) => void;
  state.turnAttachmentComposer.value = {
    finalize: () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
    clear: vi.fn(),
  };
  state.turn.value = "Первый текст";
  const saving = state.continueRun();
  state.turn.value = "Поздний текст";
  await state.continueRun();
  finish("attachments_one");
  await saving;
  expect(send).toHaveBeenCalledOnce();
  expect(send).toHaveBeenCalledWith("session_one", {
    runRef: "run_one",
    nodeRef: undefined,
    task: "Первый текст",
    attachmentSetRef: "attachments_one",
  });
});

it("не отправляет continuation после смены Run во время finalize", async () => {
  const { state, router, send } = await page();
  let finish!: (value: string) => void;
  state.turnAttachmentComposer.value = {
    finalize: () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
    clear: vi.fn(),
  };
  state.turn.value = "Первый текст";
  const saving = state.continueRun();
  await router.push("/runs/run_other");
  state.turn.value = "Новый контекст";
  finish("attachments_one");
  await saving;
  expect(send).not.toHaveBeenCalled();
  expect(state.turn.value).toBe("Новый контекст");
});

it("сохраняет исходные version/comment решения при ожидании вложений", async () => {
  const { state, platform } = await page();
  const gate = {
    ref: "gate_one",
    version: 2,
    nextActions: ["RESOLVE_GATE"],
    allowedDecisions: ["APPROVE"],
  } as OwnerGate;
  const decide = vi.spyOn(platform, "decide").mockResolvedValue(gate);
  let finish!: (value: string) => void;
  state.gateAttachmentComposers.set(gate.ref, {
    finalize: () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
    clear: vi.fn(),
  });
  state.comments.value[gate.ref] = "Первый комментарий";
  const saving = state.decide(gate, "APPROVE");
  gate.version = 3;
  state.comments.value[gate.ref] = "Поздний комментарий";
  finish("attachments_one");
  await saving;
  expect(decide).toHaveBeenCalledWith(
    expect.objectContaining({ ref: gate.ref, version: 2 }),
    {
      decision: "APPROVE",
      comment: "Первый комментарий",
      attachmentSetRef: "attachments_one",
    },
  );
});
