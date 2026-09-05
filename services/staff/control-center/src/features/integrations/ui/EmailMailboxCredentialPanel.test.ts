import { defineComponent, type Ref, type SetupContext } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import { AppProblem } from "@/shared/api/problem";
import type { IntegrationConnection } from "@/shared/api/generated/openapi/types.gen";
const dependencies = vi.hoisted(() => ({ save: vi.fn(), recover: vi.fn() }));
vi.mock("../email-credentials", async (original) => ({
  ...(await original<typeof import("../email-credentials")>()),
  saveMailboxCredential: dependencies.save,
  recoverMailboxCredential: dependencies.recover,
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
  restore(): void;
  recover(): Promise<void>;
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
beforeEach(() => {
  vi.resetAllMocks();
  const values = new Map<string, string>();
  vi.stubGlobal("window", {
    sessionStorage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    },
  });
});
afterEach(() => vi.unstubAllGlobals());
describe("EMAIL credential: очистка и неопределённость", () => {
  it("после закрытия проверяет owner receipt без value и повторного POST", async () => {
    dependencies.save.mockRejectedValue(new TypeError("Lost ACK"));
    const initial = await panel();
    initial.value.value = "synthetic private value";
    await initial.save();
    const attempt = initial.pending.value;
    initial.clear();
    const reopened = await panel();
    reopened.restore();
    expect(reopened.pending.value).toEqual(attempt);
    expect(reopened.value.value).toBe("");
    dependencies.recover.mockResolvedValue({
      name: "credential_synthetic",
      generation: 1,
      kind: "AUTH_SECRET",
      connectionRef: connection.ref,
      connectionVersion: 4,
    });
    await reopened.recover();
    expect(dependencies.save).toHaveBeenCalledOnce();
    expect(dependencies.recover.mock.calls[0]?.[0]).toEqual(attempt);
    expect(reopened.pending.value).toBeUndefined();
    expect(reopened.receipt.value).toMatchObject({ generation: 1 });
  });
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
