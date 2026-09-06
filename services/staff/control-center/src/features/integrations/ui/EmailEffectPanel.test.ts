import { defineComponent, type Ref, type SetupContext } from "vue";
import { createI18n } from "vue-i18n";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  EmailEffectReceiptView,
  EmailReconciliationOutcome,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { captureSetupState } from "@/test-utils/setup-harness";
import { emailAttemptStorageKey } from "../email-attempt";

const dependencies = vi.hoisted(() => ({
  read: vi.fn(),
  decide: vi.fn(),
  session: {
    hasPendingEmailConfirmation: vi.fn(),
    consumePendingEmailConfirmation: vi.fn(),
    finishEmailConfirmation: vi.fn(),
    beginEmailReconciliationReauth: vi.fn(),
  },
}));
vi.mock("@/features/session/store", () => ({
  useSessionStore: () => dependencies.session,
}));
vi.mock("../email-effects", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../email-effects")>()),
  readEmailEffect: dependencies.read,
  decideEmailEffect: dependencies.decide,
}));
import EmailEffectPanel from "./EmailEffectPanel.vue";

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
  nextActions: [],
  definitionVersion: "1",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
const initial: EmailEffectReceiptView = {
  receipt: {
    ref: "receipt_synthetic",
    version: 7,
    invocationRef: "invocation_synthetic",
    externalReceiptDigest: "b".repeat(64),
    semanticInputDigest: "c".repeat(64),
    outcome: "UNKNOWN_OUTCOME",
    mailboxRef: "mailbox_synthetic",
    configurationRevision: 4,
    connectionRef: connection.ref,
    projectRef: "project_synthetic",
    createdAt: "2026-09-05T00:00:00Z",
    updatedAt: "2026-09-05T00:00:00Z",
  },
};
interface PanelState {
  load(): Promise<void>;
  reconcile(): Promise<void>;
  view: Ref<EmailEffectReceiptView | undefined>;
  canDecide: Ref<boolean>;
  busy: Ref<boolean>;
  note: Ref<string>;
  outcome: Ref<EmailReconciliationOutcome | "">;
  problem: Ref<unknown>;
}
async function panel(): Promise<PanelState> {
  const setup = (
    EmailEffectPanel as unknown as {
      setup: (
        props: {
          connection: IntegrationConnection;
          initialInvocationRef: string;
        },
        context: SetupContext,
      ) => unknown;
    }
  ).setup;
  const component = defineComponent({
    setup(_props, context) {
      return setup(
        { connection, initialInvocationRef: initial.receipt.invocationRef },
        context,
      ) as Record<string, unknown>;
    },
  });
  return (await captureSetupState(component, (app) => {
    app.use(createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }));
  })) as unknown as PanelState;
}
let values: Map<string, string>;
beforeEach(() => {
  vi.resetAllMocks();
  values = new Map();
  vi.stubGlobal("window", {
    sessionStorage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => {
        values.set(key, value);
      },
      removeItem: (key: string) => {
        values.delete(key);
      },
    },
  });
  dependencies.read.mockResolvedValue(initial);
  dependencies.session.hasPendingEmailConfirmation.mockReturnValue(true);
  dependencies.session.consumePendingEmailConfirmation.mockImplementation(
    () => {
      dependencies.session.hasPendingEmailConfirmation.mockReturnValue(false);
      return true;
    },
  );
});
afterEach(() => vi.unstubAllGlobals());

describe("EmailEffectPanel: неопределённая попытка", () => {
  it("после timeout требует чтение и новое SSO, не повторяет команду и не хранит примечание", async () => {
    dependencies.decide.mockRejectedValue(new TypeError("Synthetic timeout"));
    const state = await panel();
    await state.load();
    state.note.value = "Synthetic private note";
    state.outcome.value = "NO_EFFECT_CONFIRMED";
    expect(state.canDecide.value).toBe(true);
    await state.reconcile();
    expect(state.view.value).toBeUndefined();
    expect(state.problem.value).toBeDefined();
    expect(state.busy.value).toBe(false);
    expect(dependencies.decide).toHaveBeenCalledOnce();
    expect(dependencies.read).toHaveBeenCalledOnce();
    expect(dependencies.session.finishEmailConfirmation).toHaveBeenCalledOnce();
    const metadata = values.get(emailAttemptStorageKey);
    expect(metadata).toBeTruthy();
    expect(metadata).not.toContain("Synthetic private note");
    await state.reconcile();
    expect(dependencies.decide).toHaveBeenCalledOnce();
    await state.load();
    expect(state.view.value).toEqual(initial);
    expect(state.canDecide.value).toBe(false);
    expect(values.get(emailAttemptStorageKey)).toBe(metadata);
    expect(
      dependencies.session.beginEmailReconciliationReauth,
    ).not.toHaveBeenCalled();
  });

  it("authoritative decision readback закрывает локальную неопределённую попытку без повторного RPC", async () => {
    dependencies.decide.mockRejectedValue(new TypeError("Synthetic timeout"));
    const state = await panel();
    await state.load();
    state.outcome.value = "NO_EFFECT_CONFIRMED";
    await state.reconcile();
    const resolved: EmailEffectReceiptView = {
      ...initial,
      decision: {
        ref: "decision_synthetic",
        version: 1,
        receiptRef: initial.receipt.ref,
        receiptVersion: initial.receipt.version,
        receiptDigest: initial.receipt.externalReceiptDigest,
        invocationRef: initial.receipt.invocationRef,
        outcome: "NO_EFFECT_CONFIRMED",
        actorRef: "subject_synthetic",
        createdAt: "2026-09-05T00:01:00Z",
        expiresAt: "2026-09-05T00:02:00Z",
      },
    };
    dependencies.read.mockResolvedValue(resolved);
    await state.load();
    expect(state.view.value?.decision).toEqual(resolved.decision);
    expect(state.view.value?.receipt.outcome).toBe("UNKNOWN_OUTCOME");
    expect(values.has(emailAttemptStorageKey)).toBe(false);
    expect(state.canDecide.value).toBe(false);
    expect(dependencies.decide).toHaveBeenCalledOnce();
  });
});
