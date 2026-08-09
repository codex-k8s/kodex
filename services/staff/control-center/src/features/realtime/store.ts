import { defineStore } from "pinia";
import { ref } from "vue";

import {
  RealtimeClient,
  type RealtimeEvent,
} from "@/shared/api/adapters/realtime";
import type { ProjectionChannel } from "@/shared/api/generated/asyncapi/ProjectionChannel";
import { useOperationsStore } from "@/features/operations/store";
import { useOwnerControlStore } from "@/features/owner-control/store";

export const useRealtimeStore = defineStore("realtime", () => {
  const connected = ref(false);
  const online = ref(navigator.onLine);
  const replacing = ref(true);
  const problemCode = ref<string | null>(null);
  const sequences = ref<Partial<Record<ProjectionChannel, number>>>({});
  let client: RealtimeClient | null = null;
  const expectedChannels: ProjectionChannel[] = [
    "RUNS",
    "INCIDENTS",
    "RESOURCES",
    "CONFIGURATION_CHANGES",
    "WORKSPACE_TEAMS",
    "PROVIDERS",
    "INTEGRATIONS",
    "APPROVALS",
    "BACKUPS",
    "HEALTH",
  ];
  let freshChannels = new Set<ProjectionChannel>();

  function publish(event: RealtimeEvent): void {
    if (event.type === "open") {
      connected.value = true;
      problemCode.value = null;
      sequences.value = {};
      freshChannels = new Set();
      replacing.value = true;
      return;
    }
    if (event.type === "close") {
      connected.value = false;
      replacing.value = true;
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
    freshChannels.add(event.snapshot.channel);
    replacing.value = expectedChannels.every((channel) =>
      freshChannels.has(channel),
    );
    const operations = useOperationsStore();
    const owner = useOwnerControlStore();
    if (event.snapshot.channel === "RUNS") {
      operations.replaceRealtimeRuns(event.snapshot.items.runs ?? []);
    } else if (event.snapshot.channel === "RESOURCES") {
      operations.replaceRealtimeResources(event.snapshot.items.resources ?? []);
    } else if (event.snapshot.channel === "INCIDENTS") {
      operations.replaceRealtimeIncidents(event.snapshot.items.incidents ?? []);
    } else if (event.snapshot.channel === "CONFIGURATION_CHANGES") {
      operations.replaceRealtimeChanges(
        event.snapshot.items.configurationChanges ?? [],
      );
    } else if (event.snapshot.channel === "WORKSPACE_TEAMS") {
      owner.replaceTeams(event.snapshot.items.teams ?? []);
    } else if (event.snapshot.channel === "PROVIDERS") {
      owner.replaceConnections(event.snapshot.items.providerConnections ?? []);
    } else if (event.snapshot.channel === "INTEGRATIONS") {
      owner.replaceIntegrations(
        event.snapshot.items.integrationConfigurations ?? [],
      );
    } else if (event.snapshot.channel === "APPROVALS") {
      owner.replaceApprovals(event.snapshot.items.approvals ?? []);
    } else if (event.snapshot.channel === "BACKUPS") {
      owner.replaceBackups(event.snapshot.items.resources ?? []);
    } else {
      owner.replaceHealth(event.snapshot.items.health ?? []);
    }
  }

  function start(): void {
    if (client) return;
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

  return { connected, online, replacing, problemCode, start, stop };
});
