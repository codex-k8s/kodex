import { computed, onScopeDispose, ref, watch } from "vue";
import type {
  InteractionDeliveryView,
  TelegramSubscription,
  TelegramSubscriptionInput,
} from "@/shared/api/generated/openapi/types.gen";
import {
  ownerInvalidationSignal,
  ownerRequestSignal,
} from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  enableProposal,
  type NotificationEvent,
  type ConnectionChoice,
} from "./model";
import * as api from "./api";

export type TelegramNotificationsAPI = typeof api;

// Состояние принадлежит одному открытому project/owner scope. Poll не заменяет ввод и OCC baseline.
export function useTelegramNotifications(
  project: () => string,
  port: TelegramNotificationsAPI = api,
) {
  const subscription = ref<TelegramSubscription>();
  const baseline = ref<number>();
  const ready = ref(false);
  const canManage = ref(false);
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const refreshRequired = ref(false);
  const saved = ref(false);
  const connections = ref<ConnectionChoice[]>([]);
  const connectionPage = ref<string>();
  const deliveries = ref<InteractionDeliveryView[]>([]);
  const deliveryPage = ref<string>();
  const connectionRef = ref("");
  const hours = ref(24);
  const maxEffects = ref(100);
  const events = ref<NotificationEvent[]>(["FINISHED", "FAILED", "HUMAN_GATE"]);
  const owner = ownerInvalidationSignal();
  let active = !owner.aborted;
  let generation = 0;
  let request: AbortController | undefined;
  let poll: ReturnType<typeof setInterval> | undefined;
  let failures = 0;
  const stale = computed(
    () => ready.value && baseline.value !== subscription.value?.version,
  );
  const mayWrite = computed(
    () =>
      active &&
      ready.value &&
      canManage.value &&
      !busy.value &&
      !refreshRequired.value &&
      !stale.value,
  );
  const valid = computed(() => {
    try {
      enableProposal(
        connectionRef.value,
        events.value,
        hours.value,
        maxEffects.value,
      );
      return connections.value.some((item) => item.ref === connectionRef.value);
    } catch {
      return false;
    }
  });

  function clear() {
    generation++;
    request?.abort();
    request = undefined;
    if (poll) clearInterval(poll);
    poll = undefined;
    failures = 0;
    subscription.value = undefined;
    baseline.value = undefined;
    ready.value = false;
    canManage.value = false;
    busy.value = false;
    problem.value = undefined;
    refreshRequired.value = false;
    saved.value = false;
    connections.value = [];
    deliveries.value = [];
    connectionPage.value = undefined;
    deliveryPage.value = undefined;
    connectionRef.value = "";
    hours.value = 24;
    maxEffects.value = 100;
    events.value = ["FINISHED", "FAILED", "HUMAN_GATE"];
  }
  function invalidate() {
    active = false;
    clear();
  }
  owner.addEventListener("abort", invalidate, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", invalidate);
    invalidate();
  });

  function begin() {
    const controller = new AbortController();
    request = controller;
    const signal = AbortSignal.any([controller.signal, ownerRequestSignal()]);
    const ticket = ++generation;
    busy.value = true;
    return {
      signal,
      current: () => active && !signal.aborted && ticket === generation,
      finish: () => {
        if (ticket === generation) busy.value = false;
      },
    };
  }
  function failed(error: unknown) {
    problem.value = asProblem(error);
    failures++;
    if (
      ["forbidden", "unauthorized", "not-found"].includes(problem.value.kind)
    ) {
      canManage.value = false;
      subscription.value = undefined;
      baseline.value = undefined;
      deliveries.value = [];
      connections.value = [];
      ready.value = false;
    }
    if (failures >= 3 && poll) {
      clearInterval(poll);
      poll = undefined;
    }
  }
  async function refresh(explicit = true) {
    if (!active || busy.value || !project() || ownerRequestSignal().aborted)
      return;
    const scope = project();
    const operation = begin();
    if (explicit) {
      problem.value = undefined;
      saved.value = false;
    }
    try {
      const value = await port.readSubscription(scope, operation.signal);
      if (!operation.current() || scope !== project()) return;
      const [history, choices] = await Promise.all([
        port.readDeliveries(scope, undefined, operation.signal),
        explicit && value.canManage
          ? port.readConnections(undefined, operation.signal)
          : Promise.resolve(undefined),
      ]);
      if (!operation.current() || scope !== project()) return;
      subscription.value = value.subscription;
      canManage.value = value.canManage;
      deliveries.value = history.items;
      deliveryPage.value = history.nextPageToken;
      if (explicit) {
        baseline.value = value.subscription?.version;
        connectionRef.value = value.subscription?.connectionRef ?? "";
        events.value = value.subscription
          ? [...value.subscription.eventTypes]
          : ["FINISHED", "FAILED", "HUMAN_GATE"];
        // Это предложение нового срока, а не оставшийся срок или автоматическое продление.
        hours.value = 24;
        maxEffects.value = value.subscription?.maxEffects ?? 100;
        connections.value = choices?.items ?? [];
        connectionPage.value = choices?.nextPageToken;
        refreshRequired.value = false;
      }
      ready.value = true;
      failures = 0;
      if (!poll)
        poll = setInterval(() => {
          void refresh(false);
        }, 15000);
    } catch (error) {
      if (operation.current()) failed(error);
    } finally {
      operation.finish();
    }
  }
  async function save(enabled: boolean) {
    if (
      !mayWrite.value ||
      (enabled && !valid.value) ||
      (!enabled && !subscription.value?.enabled)
    )
      return;
    const scope = project();
    const input: TelegramSubscriptionInput = enabled
      ? enableProposal(
          connectionRef.value,
          events.value,
          hours.value,
          maxEffects.value,
        )
      : { enabled: false };
    const operation = begin();
    refreshRequired.value = true;
    problem.value = undefined;
    saved.value = false;
    try {
      const value = await port.saveSubscription(
        scope,
        input,
        baseline.value,
        operation.signal,
      );
      if (!operation.current() || scope !== project()) return;
      subscription.value = value;
      baseline.value = value.version;
      canManage.value = value.canManage;
      refreshRequired.value = false;
      saved.value = true;
    } catch (error) {
      if (operation.current()) failed(error);
    } finally {
      operation.finish();
    }
  }
  async function more(kind: "connections" | "deliveries") {
    const token =
      kind === "connections" ? connectionPage.value : deliveryPage.value;
    const existing =
      kind === "connections" ? connections.value : deliveries.value;
    if (
      !active ||
      busy.value ||
      !ready.value ||
      !token ||
      existing.length >= 100 ||
      ownerRequestSignal().aborted
    )
      return;
    const operation = begin();
    const scope = project();
    try {
      if (kind === "connections") {
        const page = await port.readConnections(token, operation.signal);
        if (!operation.current() || scope !== project()) return;
        if (page.nextPageToken === token)
          throw new Error("Telegram connection cursor did not advance");
        const merged = new Map(
          connections.value.map((item) => [item.ref, item]),
        );
        for (const item of page.items) merged.set(item.ref, item);
        connections.value = [...merged.values()].slice(0, 100);
        connectionPage.value = page.nextPageToken;
      } else {
        const page = await port.readDeliveries(scope, token, operation.signal);
        if (!operation.current() || scope !== project()) return;
        if (page.nextPageToken === token)
          throw new Error("Interaction delivery cursor did not advance");
        const merged = new Map(
          deliveries.value.map((item) => [item.ref, item]),
        );
        for (const item of page.items) merged.set(item.ref, item);
        deliveries.value = [...merged.values()].slice(0, 100);
        deliveryPage.value = page.nextPageToken;
      }
    } catch (error) {
      if (operation.current()) failed(error);
    } finally {
      operation.finish();
    }
  }
  watch(
    project,
    () => {
      clear();
      if (active) void refresh();
    },
    { immediate: true },
  );
  return {
    subscription,
    ready,
    canManage,
    busy,
    problem,
    refreshRequired,
    saved,
    stale,
    mayWrite,
    valid,
    connections,
    connectionPage,
    deliveries,
    deliveryPage,
    connectionRef,
    hours,
    maxEffects,
    events,
    refresh,
    save,
    more,
  };
}
