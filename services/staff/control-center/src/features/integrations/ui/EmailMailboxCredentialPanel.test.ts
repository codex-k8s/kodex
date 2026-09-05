import { defineComponent, type Ref, type SetupContext } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import { AppProblem } from "@/shared/api/problem";
import type { IntegrationConnection } from "@/shared/api/generated/openapi/types.gen";
const dependencies = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("../email-credentials", async (original) => ({
  ...(await original<typeof import("../email-credentials")>()),
  saveMailboxCredential: dependencies.save,
}));
import EmailMailboxCredentialPanel from "./EmailMailboxCredentialPanel.vue";
const connection: IntegrationConnection = {
  ref: "connection_synthetic",
  version: 3,
  definitionKey: "email",
  name: "Email",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: ["CONFIGURE_CREDENTIAL"],
  definitionVersion: "1",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
interface State {
  save(): Promise<void>;
  clear(): void;
  value: Ref<string>;
  busy: Ref<boolean>;
  pending: Ref<unknown>;
  mismatch: Ref<boolean>;
  problem: Ref<AppProblem | undefined>;
  receipt: Ref<unknown>;
}
async function panel(): Promise<State> {
  const setup = (
    EmailMailboxCredentialPanel as unknown as {
      setup: (
        props: { connection: IntegrationConnection },
        context: SetupContext,
      ) => Record<string, unknown>;
    }
  ).setup;
  return (await captureSetupState(
    defineComponent({
      setup(_props, context) {
        return setup({ connection }, context);
      },
    }),
  )) as unknown as State;
}
beforeEach(() => vi.resetAllMocks());
describe("EMAIL credential: очистка и неопределённость", () => {
  it("очищает ввод до ответа; timeout не повторяет команду и требует точное значение", async () => {
    dependencies.save.mockRejectedValue(new TypeError("Synthetic timeout"));
    const state = await panel();
    state.value.value = "synthetic credential";
    const operation = state.save();
    expect(state.value.value).toBe("");
    expect(state.busy.value).toBe(true);
    await operation;
    expect(dependencies.save).toHaveBeenCalledOnce();
    expect(JSON.stringify(state.pending.value)).not.toContain(
      "synthetic credential",
    );
    await state.save();
    expect(dependencies.save).toHaveBeenCalledOnce();
    state.value.value = "different";
    await state.save();
    expect(state.mismatch.value).toBe(true);
    expect(dependencies.save).toHaveBeenCalledOnce();
    state.value.value = "synthetic credential";
    await state.save();
    expect(dependencies.save).toHaveBeenCalledTimes(2);
    expect(dependencies.save.mock.calls[0]?.[0]).toEqual(
      dependencies.save.mock.calls[1]?.[0],
    );
  });
  it("после очистки не принимает поздний ответ", async () => {
    let resolve: ((value: unknown) => void) | undefined;
    dependencies.save.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const state = await panel();
    state.value.value = "synthetic credential";
    const operation = state.save();
    await vi.waitFor(() => expect(dependencies.save).toHaveBeenCalledOnce());
    state.clear();
    resolve?.({ name: "credential_synthetic" });
    await operation;
    expect(state.receipt.value).toBeUndefined();
    expect(state.pending.value).toBeUndefined();
    expect(state.value.value).toBe("");
    expect(state.busy.value).toBe(false);
  });
  it("не показывает потенциальное значение в диагностике и разрешает исправление отклонённого ввода", async () => {
    dependencies.save.mockRejectedValue(
      new AppProblem({
        status: 400,
        code: "INVALID_ARGUMENT",
        kind: "unknown",
        retryable: false,
        detail: "synthetic private value",
      }),
    );
    const state = await panel();
    state.value.value = "synthetic private value";
    await state.save();
    expect(state.problem.value?.code).toBe("INVALID_ARGUMENT");
    expect(state.problem.value?.detail).toBeUndefined();
    expect(state.pending.value).toBeUndefined();
  });
});
