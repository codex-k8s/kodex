import type { ConfigurationChange } from "@/shared/api/generated/asyncapi/ConfigurationChange";
import type { ProjectionChannel } from "@/shared/api/generated/asyncapi/ProjectionChannel";
import type { Resource as AsyncResource } from "@/shared/api/generated/asyncapi/Resource";
import type { RuntimeIncident as AsyncRuntimeIncident } from "@/shared/api/generated/asyncapi/RuntimeIncident";
import type { SnapshotEnvelope } from "@/shared/api/generated/asyncapi/SnapshotEnvelope";
import type { SubscribeEnvelope } from "@/shared/api/generated/asyncapi/SubscribeEnvelope";
import type {
  ConfigurationChange as ApiConfigurationChange,
  LifecycleState,
  Resource,
  ResourceKind,
  RuntimeIncident,
} from "@/shared/api/generated/openapi/types.gen";
import { runtimeConfig } from "@/shared/config/runtime";
import { csrfToken } from "@/shared/lib/identity";

export type RealtimeSnapshot = Omit<
  SnapshotEnvelope,
  "reservedType" | "items"
> & {
  type: "SNAPSHOT";
  channel: ProjectionChannel;
  items: {
    resources?: Resource[];
    incidents?: RuntimeIncident[];
    configurationChanges?: ConfigurationChange[];
  };
};

export type RealtimeEvent =
  | { type: "open" }
  | { type: "close" }
  | { type: "snapshot"; snapshot: RealtimeSnapshot }
  | { type: "problem"; code: string; retryable: boolean };

const channels: ProjectionChannel[] = [
  "RUNS",
  "INCIDENTS",
  "RESOURCES",
  "CONFIGURATION_CHANGES",
];
const resourceKinds: ResourceKind[] = [
  "PROJECT",
  "TEAM",
  "CHAT",
  "ROLE",
  "PROMPT_PROFILE",
  "CREDENTIAL_BINDING",
  "REPOSITORY_WORKSPACE",
  "INTEGRATION",
  "RUNTIME_REVISION",
  "SESSION",
  "TURN",
  "PROCESS_RUN",
  "SCHEDULE",
  "OWNER_GATE",
  "MEMORY_RECORD",
  "WORK_CLAIM",
  "ARTIFACT",
  "ROLE_IMAGE_RECIPE",
  "IMAGE_BUILD",
  "IMAGE_ARTIFACT",
];
const lifecycleStates: LifecycleState[] = [
  "ACTIVE",
  "PAUSED",
  "ARCHIVED",
  "DELETION_PENDING",
  "DELETED",
  "QUEUED",
  "CLAIMED",
  "RUNNING",
  "WAITING_OWNER",
  "WAITING_EXTERNAL",
  "SUCCEEDED",
  "FAILED",
  "CANCELLED",
  "EXPIRED",
  "BLOCKED",
];
const incidentKinds = [
  "HEARTBEAT_MISSED",
  "RECONCILE_FAILED",
  "WORKLOAD_UNAVAILABLE",
] as const;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function mapResource(value: unknown): Resource | null {
  if (
    !isRecord(value) ||
    typeof value.id !== "string" ||
    !resourceKinds.includes(value.kind as ResourceKind) ||
    typeof value.name !== "string" ||
    !lifecycleStates.includes(value.state as LifecycleState) ||
    !Number.isSafeInteger(value.version) ||
    !isRecord(value.spec) ||
    typeof value.createdAt !== "string" ||
    typeof value.updatedAt !== "string"
  )
    return null;
  const generated: AsyncResource = {
    id: value.id,
    kind: value.kind as AsyncResource["kind"],
    reservedName: value.name,
    state: value.state as AsyncResource["state"],
    version: value.version as number,
    ...(typeof value.projectId === "string"
      ? { projectId: value.projectId }
      : {}),
    ...(typeof value.parentId === "string" ? { parentId: value.parentId } : {}),
    spec: value.spec as AsyncResource["spec"],
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
  const { reservedName, ...resource } = generated;
  return {
    ...resource,
    name: reservedName,
    spec: resource.spec as Resource["spec"],
  };
}

function isIncident(value: unknown): value is AsyncRuntimeIncident {
  return (
    isRecord(value) &&
    typeof value.incidentId === "string" &&
    typeof value.executionId === "string" &&
    Number.isSafeInteger(value.executionFence) &&
    incidentKinds.includes(value.kind as (typeof incidentKinds)[number]) &&
    typeof value.evidenceSha256 === "string" &&
    typeof value.workloadId === "string" &&
    typeof value.occurredAt === "string"
  );
}

function isConfigurationChange(value: unknown): value is ConfigurationChange {
  return (
    isRecord(value) &&
    typeof value.id === "string" &&
    typeof value.action === "string" &&
    typeof value.resourceId === "string" &&
    resourceKinds.includes(value.resourceKind as ResourceKind) &&
    Number.isSafeInteger(value.resourceVersion) &&
    value.outcome === "succeeded" &&
    typeof value.actorId === "string" &&
    typeof value.correlationId === "string" &&
    Number.isSafeInteger(value.policyRevision) &&
    typeof value.occurredAt === "string"
  );
}

export function parseRealtimeMessage(raw: string): RealtimeEvent | null {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isRecord(value) || typeof value.type !== "string") return null;
  if (value.type === "PROBLEM") {
    return typeof value.code === "string" &&
      typeof value.retryable === "boolean"
      ? { type: "problem", code: value.code, retryable: value.retryable }
      : null;
  }
  if (
    value.type !== "SNAPSHOT" ||
    typeof value.requestId !== "string" ||
    !channels.includes(value.channel as ProjectionChannel) ||
    !Number.isSafeInteger(value.sequence) ||
    (value.sequence as number) < 1 ||
    typeof value.snapshotId !== "string" ||
    value.complete !== true ||
    typeof value.serverTime !== "string" ||
    !isRecord(value.items)
  ) {
    return null;
  }
  const itemKeys = Object.keys(value.items);
  if (
    itemKeys.length !== 1 ||
    !["resources", "incidents", "configurationChanges"].includes(
      itemKeys[0] ?? "",
    )
  )
    return null;
  const items = value.items;
  const itemList = items[itemKeys[0] ?? ""];
  if (!Array.isArray(itemList)) return null;
  const channel = value.channel as ProjectionChannel;
  const expectedItemsKey =
    channel === "INCIDENTS"
      ? "incidents"
      : channel === "CONFIGURATION_CHANGES"
        ? "configurationChanges"
        : "resources";
  if (itemKeys[0] !== expectedItemsKey) return null;
  const snapshotItems: RealtimeSnapshot["items"] = {};
  if (itemKeys[0] === "resources") {
    const resources = itemList.map(mapResource);
    if (resources.some((item) => item === null)) return null;
    snapshotItems.resources = resources as Resource[];
  } else if (itemKeys[0] === "incidents") {
    if (!itemList.every(isIncident)) return null;
    snapshotItems.incidents = itemList as RuntimeIncident[];
  } else {
    if (!itemList.every(isConfigurationChange)) return null;
    snapshotItems.configurationChanges = itemList as ApiConfigurationChange[];
  }
  return {
    type: "snapshot",
    snapshot: {
      type: "SNAPSHOT",
      requestId: value.requestId,
      channel,
      sequence: value.sequence as number,
      snapshotId: value.snapshotId,
      complete: true,
      serverTime: value.serverTime,
      items: snapshotItems,
    },
  };
}

export class RealtimeClient {
  private socket: WebSocket | null = null;
  private reconnectTimer: number | null = null;
  private stopped = true;
  private attempt = 0;

  constructor(private readonly publish: (event: RealtimeEvent) => void) {}

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.connect();
  }

  stop(): void {
    this.stopped = true;
    if (this.reconnectTimer !== null) window.clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
    this.socket?.close(1000, "client shutdown");
    this.socket = null;
  }

  private connect(): void {
    if (this.stopped) return;
    let token: string;
    try {
      token = csrfToken();
    } catch {
      this.scheduleReconnect();
      return;
    }
    const socket = new WebSocket(runtimeConfig().realtimeUrl, [
      "mattercodex.control.v1",
      `csrf.${token}`,
    ]);
    this.socket = socket;
    socket.addEventListener("open", () => {
      if (this.socket !== socket || this.stopped) return;
      this.attempt = 0;
      const subscribe: SubscribeEnvelope = {
        reservedType: "SUBSCRIBE",
        requestId: crypto.randomUUID(),
        channels,
      };
      socket.send(
        JSON.stringify({
          type: subscribe.reservedType,
          requestId: subscribe.requestId,
          channels: subscribe.channels,
        }),
      );
      this.publish({ type: "open" });
    });
    socket.addEventListener("message", (event) => {
      if (this.socket !== socket || typeof event.data !== "string") return;
      const message = parseRealtimeMessage(event.data);
      if (!message) {
        socket.close(1008, "invalid realtime envelope");
        return;
      }
      this.publish(message);
    });
    socket.addEventListener("close", () => {
      if (this.socket !== socket) return;
      this.socket = null;
      this.publish({ type: "close" });
      this.scheduleReconnect();
    });
  }

  private scheduleReconnect(): void {
    if (this.stopped || this.reconnectTimer !== null) return;
    const delay = Math.min(30_000, 1_000 * 2 ** Math.min(this.attempt, 5));
    this.attempt += 1;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }
}
