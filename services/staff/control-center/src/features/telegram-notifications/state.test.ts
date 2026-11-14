import { effectScope, nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TelegramSubscription } from "@/shared/api/generated/openapi/types.gen";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { subscriptionView, enableProposal } from "./model";
import {
  useTelegramNotifications,
  type TelegramNotificationsAPI,
} from "./state";

const current: TelegramSubscription = {
  ref: "tgs_fixture01",
  projectRef: "prj_fixture01",
  connectionRef: "int_fixture01",
  version: 2,
  enabled: true,
  canManage: true,
  state: "ACTIVE",
  eventTypes: ["FINISHED", "FAILED", "HUMAN_GATE"],
  scopeDigest: "a".repeat(64),
  botId: 12345678,
  chatDisplay: "…1234",
  topicDisplay: "NONE",
  expiresAt: "2026-11-15T12:00:00Z",
  updatedAt: "2026-11-14T12:00:00Z",
  maxEffects: 100,
  usedEffects: 0,
};
const scopes: ReturnType<typeof effectScope>[] = [];
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
async function settle() {
  for (let index = 0; index < 8; index++) await Promise.resolve();
  await nextTick();
}
function fixture() {
  const port = {
    readSubscription: vi
      .fn<TelegramNotificationsAPI["readSubscription"]>()
      .mockResolvedValue({ subscription: current, canManage: true }),
    readDeliveries: vi
      .fn<TelegramNotificationsAPI["readDeliveries"]>()
      .mockResolvedValue({ items: [], nextPageToken: undefined }),
    readConnections: vi
      .fn<TelegramNotificationsAPI["readConnections"]>()
      .mockResolvedValue({
        items: [{ ref: "int_fixture01", name: "Fixture service bot" }],
        nextPageToken: undefined,
      }),
    saveSubscription: vi
      .fn<TelegramNotificationsAPI["saveSubscription"]>()
      .mockResolvedValue({ ...current, version: 3 }),
  };
  const scope = effectScope();
  scopes.push(scope);
  const project = ref("prj_fixture01");
  const state = scope.run(() =>
    useTelegramNotifications(() => project.value, port),
  );
  if (!state) throw new Error("Telegram fixture scope is unavailable");
  return { state, project, port, scope };
}
beforeEach(() => {
  resetOwnerRequests();
  vi.useFakeTimers();
});
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop();
  resetOwnerRequests();
  vi.useRealTimers();
});

describe("Подписка Telegram: owner lifecycle UI", () => {
  it("предлагает 24 часа без автоматической записи; явный enable отправляет точную область, OCC и лимит один раз", async () => {
    const { state, port } = fixture();
    await settle();
    expect(state.ready.value).toBe(true);
    expect(state.hours.value).toBe(24);
    expect(port.saveSubscription).not.toHaveBeenCalled();
    state.hours.value = 72;
    state.maxEffects.value = 25;
    state.events.value = ["FAILED"];
    await state.save(true);
    expect(port.saveSubscription).toHaveBeenCalledOnce();
    expect(port.saveSubscription.mock.calls[0]?.slice(0, 3)).toEqual([
      "prj_fixture01",
      {
        enabled: true,
        connectionRef: "int_fixture01",
        eventTypes: ["FAILED"],
        durationSeconds: 259200,
        maxEffects: 25,
      },
      2,
    ]);
    expect(state.subscription.value?.version).toBe(3);
    expect(state.saved.value).toBe(true);
    await vi.advanceTimersByTimeAsync(60000);
    expect(port.saveSubscription).toHaveBeenCalledOnce();
  });
  it("сохраняет ввод и прежний OCC при polling новой версии; явное чтение снимает конфликт", async () => {
    const { state, port } = fixture();
    await settle();
    state.hours.value = 48;
    state.events.value = ["FAILED"];
    port.readSubscription.mockResolvedValue({
      subscription: { ...current, version: 3 },
      canManage: true,
    });
    await vi.advanceTimersByTimeAsync(15000);
    expect(state.stale.value).toBe(true);
    expect(state.hours.value).toBe(48);
    expect(state.events.value).toEqual(["FAILED"]);
    await state.save(true);
    expect(port.saveSubscription).not.toHaveBeenCalled();
    await state.refresh();
    expect(state.stale.value).toBe(false);
    expect(state.hours.value).toBe(24);
  });
  it("после UNKNOWN не повторяет mutation и не снимает блокировку от фонового чтения", async () => {
    const { state, port } = fixture();
    await settle();
    port.saveSubscription.mockRejectedValue(
      new Error("Synthetic connection closed"),
    );
    await state.save(true);
    expect(state.refreshRequired.value).toBe(true);
    await vi.advanceTimersByTimeAsync(30000);
    await state.save(true);
    expect(port.saveSubscription).toHaveBeenCalledOnce();
    expect(state.refreshRequired.value).toBe(true);
    await state.refresh();
    expect(state.refreshRequired.value).toBe(false);
  });
  it("отзывает scope при logout и не применяет поздний успешный ACK", async () => {
    const { state, port } = fixture();
    await settle();
    const pending = deferred<TelegramSubscription>();
    port.saveSubscription.mockReturnValue(pending.promise);
    const saving = state.save(true);
    resetOwnerRequests();
    expect(port.saveSubscription.mock.calls[0]?.[3].aborted).toBe(true);
    pending.resolve({ ...current, version: 3 });
    await saving;
    expect(state.subscription.value).toBeUndefined();
    expect(state.ready.value).toBe(false);
    expect(state.saved.value).toBe(false);
    await vi.advanceTimersByTimeAsync(60000);
    expect(port.saveSubscription).toHaveBeenCalledOnce();
  });
  it("смена проекта отменяет предыдущий read и исключает поздние данные другого проекта", async () => {
    const { state, port, project } = fixture();
    await settle();
    const pending =
      deferred<
        Awaited<ReturnType<TelegramNotificationsAPI["readSubscription"]>>
      >();
    port.readSubscription.mockReturnValueOnce(pending.promise);
    const refreshing = state.refresh();
    const oldSignal = port.readSubscription.mock.calls.at(-1)?.[1];
    port.readSubscription.mockResolvedValue({
      subscription: undefined,
      canManage: false,
    });
    project.value = "prj_other001";
    await settle();
    expect(oldSignal?.aborted).toBe(true);
    pending.resolve({ subscription: current, canManage: true });
    await refreshing;
    expect(state.subscription.value).toBeUndefined();
    expect(state.canManage.value).toBe(false);
  });
  it("явный unconfigured остаётся пустым состоянием, readonly не запрашивает управляемые подключения", async () => {
    const { state, port } = fixture();
    await settle();
    port.readSubscription.mockResolvedValue({
      subscription: undefined,
      canManage: false,
    });
    port.readConnections.mockClear();
    await state.refresh();
    expect(state.ready.value).toBe(true);
    expect(state.subscription.value).toBeUndefined();
    expect(state.mayWrite.value).toBe(false);
    expect(port.readConnections).not.toHaveBeenCalled();
  });
});

describe("Telegram public model", () => {
  it("отвергает неполные/чужие metadata и не копирует будущие private поля", () => {
    expect(
      subscriptionView(
        { ...current, privateToken: "synthetic" } as TelegramSubscription,
        current.projectRef,
      ),
    ).not.toHaveProperty("privateToken");
    for (const value of [
      { ...current, projectRef: "prj_foreign01" },
      { ...current, chatDisplay: "-10012345678" },
      { ...current, usedEffects: undefined },
      { ...current, eventTypes: ["FINISHED", "FINISHED"] },
    ])
      expect(() =>
        subscriptionView(value as TelegramSubscription, current.projectRef),
      ).toThrow();
    expect(() =>
      enableProposal("int_fixture01", ["FINISHED"], 169, 100),
    ).toThrow();
    expect(() => enableProposal("int_fixture01", [], 24, 100)).toThrow();
  });
});
