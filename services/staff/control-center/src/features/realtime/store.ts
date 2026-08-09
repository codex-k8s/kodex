import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  RealtimeClient,
  realtimeChannels,
  type RealtimeEvent,
} from "@/shared/api/adapters/realtime";
import type { ProjectionChannel } from "@/shared/api/generated/asyncapi/ProjectionChannel";
import { notifyAuthoritativeUnauthorized } from "@/shared/api/problem";

export const useRealtimeStore = defineStore("realtime", () => {
  const connected = ref(false);
  const online = ref(navigator.onLine);
  const replacing = ref(true);
  const problemCode = ref<string | null>(null);
  const sequences = ref<Partial<Record<ProjectionChannel, number>>>({});
  const generation = ref(0);
  const ready = computed(
    () => online.value && connected.value && !replacing.value,
  );
  let client: RealtimeClient | null = null;
  let freshChannels = new Set<ProjectionChannel>();

  function publish(event: RealtimeEvent): void {
    if (event.type === "generation") {
      generation.value = event.generation;
      connected.value = false;
      problemCode.value = null;
      sequences.value = {};
      freshChannels = new Set();
      replacing.value = true;
      return;
    }
    if (event.type === "open") {
      if (event.generation !== generation.value) return;
      connected.value = true;
      problemCode.value = null;
      return;
    }
    if (event.type === "close") {
      if (event.generation !== generation.value) return;
      connected.value = false;
      replacing.value = true;
      window.dispatchEvent(new Event("mattercodex:realtime-disconnected"));
      return;
    }
    if (event.type === "problem") {
      problemCode.value = event.code;
      replacing.value = true;
      if (event.code === "UNAUTHENTICATED" && !event.retryable)
        notifyAuthoritativeUnauthorized();
      return;
    }
    if (event.generation !== generation.value) return;
    const previous = sequences.value[event.snapshot.channel] ?? 0;
    if (event.snapshot.sequence <= previous) return;
    sequences.value = {
      ...sequences.value,
      [event.snapshot.channel]: event.snapshot.sequence,
    };
    freshChannels.add(event.snapshot.channel);
    replacing.value = !realtimeChannels.every((channel) =>
      freshChannels.has(channel),
    );
    window.dispatchEvent(
      new CustomEvent("mattercodex:realtime-snapshot", {
        detail: event.snapshot,
      }),
    );
  }

  function start(): void {
    if (client) return;
    online.value = navigator.onLine;
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    client = new RealtimeClient(publish);
    client.start();
  }

  function stop(): void {
    window.removeEventListener("online", handleOnline);
    window.removeEventListener("offline", handleOffline);
    client?.stop();
    client = null;
    connected.value = false;
    replacing.value = true;
    sequences.value = {};
    freshChannels = new Set();
  }

  function handleOnline(): void {
    online.value = true;
    client?.stop();
    client?.start();
  }

  function handleOffline(): void {
    online.value = false;
    connected.value = false;
    replacing.value = true;
  }

  function reset(): void {
    stop();
    problemCode.value = null;
    generation.value = 0;
  }

  return {
    connected,
    online,
    replacing,
    ready,
    problemCode,
    start,
    stop,
    reset,
  };
});
