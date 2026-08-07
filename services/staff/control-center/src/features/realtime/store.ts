import { defineStore } from "pinia";
import { ref } from "vue";

import {
  RealtimeClient,
  type RealtimeEvent,
} from "@/shared/api/adapters/realtime";
import type { ProjectionChannel } from "@/shared/api/generated/asyncapi/ProjectionChannel";
import { useOperationsStore } from "@/features/operations/store";

export const useRealtimeStore = defineStore("realtime", () => {
  const connected = ref(false);
  const problemCode = ref<string | null>(null);
  const sequences = ref<Partial<Record<ProjectionChannel, number>>>({});
  let client: RealtimeClient | null = null;

  function publish(event: RealtimeEvent): void {
    if (event.type === "open") {
      connected.value = true;
      problemCode.value = null;
      sequences.value = {};
      return;
    }
    if (event.type === "close") {
      connected.value = false;
      return;
    }
    if (event.type === "problem") {
      problemCode.value = event.code;
      return;
    }
    const previous = sequences.value[event.snapshot.channel] ?? 0;
    if (event.snapshot.sequence <= previous) return;
    sequences.value = {
      ...sequences.value,
      [event.snapshot.channel]: event.snapshot.sequence,
    };
    const operations = useOperationsStore();
    if (
      event.snapshot.channel === "RUNS" ||
      event.snapshot.channel === "RESOURCES"
    ) {
      operations.replaceRealtimeResources(
        event.snapshot.channel,
        event.snapshot.items.resources ?? [],
      );
    } else if (event.snapshot.channel === "INCIDENTS") {
      operations.replaceRealtimeIncidents(event.snapshot.items.incidents ?? []);
    } else {
      operations.replaceRealtimeChanges(
        event.snapshot.items.configurationChanges ?? [],
      );
    }
  }

  function start(): void {
    if (client) return;
    client = new RealtimeClient(publish);
    client.start();
  }

  function stop(): void {
    client?.stop();
    client = null;
    connected.value = false;
  }

  return { connected, problemCode, start, stop };
});
