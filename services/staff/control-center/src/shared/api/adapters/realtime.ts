import type { IncidentProjection } from "@/shared/api/generated/asyncapi/IncidentProjection";
import type { IntegrationConfigurationProjection } from "@/shared/api/generated/asyncapi/IntegrationConfigurationProjection";
import type { ProjectionChannel } from "@/shared/api/generated/asyncapi/ProjectionChannel";
import type { ProviderConnectionProjection } from "@/shared/api/generated/asyncapi/ProviderConnectionProjection";
import type { Resource as AsyncResource } from "@/shared/api/generated/asyncapi/Resource";
import type { RunProjection } from "@/shared/api/generated/asyncapi/RunProjection";
import type { SnapshotEnvelope } from "@/shared/api/generated/asyncapi/SnapshotEnvelope";
import type { SubscribeEnvelope } from "@/shared/api/generated/asyncapi/SubscribeEnvelope";
import type {
  ConfigurationChange,
  HealthObservation,
  IncidentView,
  IntegrationApproval,
  IntegrationConfiguration,
  LifecycleState,
  MattermostTeam,
  ProviderConnection,
  Resource,
  ResourceKind,
  RunView,
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
    runs?: RunView[];
    resources?: Resource[];
    incidents?: IncidentView[];
    configurationChanges?: ConfigurationChange[];
    teams?: MattermostTeam[];
    providerConnections?: ProviderConnection[];
    integrationConfigurations?: IntegrationConfiguration[];
    approvals?: IntegrationApproval[];
    health?: HealthObservation[];
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
  "WORKSPACE_TEAMS",
  "PROVIDERS",
  "INTEGRATIONS",
  "APPROVALS",
  "BACKUPS",
  "HEALTH",
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
  "ROLE_DEFINITION",
  "AGENT",
  "AGENT_ASSIGNMENT",
  "INSTRUCTION_SET",
  "PROVIDER_CONNECTION_REFERENCE",
  "PROVIDER_POOL",
  "WORKSPACE_BACKUP",
  "WORKSPACE_RESTORE",
  "WORKSPACE_MATTERMOST_MAPPING",
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
const configurationActions: ConfigurationChange["action"][] = [
  "create",
  "update",
  "transition",
  "delete",
  "update_project",
  "delete_project_pending",
  "delete_project",
  "detach_access_configuration",
  "copy_access_configuration",
  "create_schedule",
  "manage_schedule_UPDATE",
  "manage_schedule_ACTIVATE",
  "manage_schedule_PAUSE",
  "manage_schedule_ARCHIVE",
  "manage_schedule_DELETE_ARCHIVE",
  "manage_schedule_DELETE_PENDING",
  "manage_schedule_DELETE",
];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isVersion(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) > 0;
}

function mapDisplay(value: unknown): RunView["workspace"] | null {
  if (
    !isRecord(value) ||
    !["PRESENT", "UNAVAILABLE", "STALE", "INELIGIBLE"].includes(
      String(value.status),
    ) ||
    typeof value.value !== "string"
  )
    return null;
  return {
    status: value.status as RunView["workspace"]["status"],
    value: value.value,
  };
}

function mapResource(value: unknown): Resource | null {
  if (
    !isRecord(value) ||
    typeof value.id !== "string" ||
    !resourceKinds.includes(value.kind as ResourceKind) ||
    typeof value.name !== "string" ||
    !lifecycleStates.includes(value.state as LifecycleState) ||
    !isVersion(value.version) ||
    typeof value.projectionSha256 !== "string" ||
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
    version: value.version,
    projectionSha256: value.projectionSha256,
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

function mapRun(value: unknown): RunView | null {
  if (
    !isRecord(value) ||
    typeof value.runRef !== "string" ||
    typeof value.displayName !== "string" ||
    !isVersion(value.version) ||
    !lifecycleStates.includes(value.state as LifecycleState) ||
    !Array.isArray(value.nextActions) ||
    typeof value.updatedAt !== "string" ||
    typeof value.attempt !== "number" ||
    typeof value.durationSeconds !== "number"
  )
    return null;
  const workspace = mapDisplay(value.workspace);
  const trigger = mapDisplay(value.trigger);
  const runtimeStatus = mapDisplay(value.runtimeStatus);
  const initiator = mapDisplay(value.initiator);
  const agent = mapDisplay(value.agent);
  const role = mapDisplay(value.role);
  const model = mapDisplay(value.model);
  const provider = mapDisplay(value.provider);
  if (
    !workspace ||
    !trigger ||
    !runtimeStatus ||
    !initiator ||
    !agent ||
    !role ||
    !model ||
    !provider
  )
    return null;
  const generated = value as unknown as RunProjection;
  return {
    runRef: generated.runRef,
    displayName: generated.displayName,
    version: generated.version,
    state: generated.state,
    workspace,
    trigger,
    runtimeStatus,
    attempt: generated.attempt,
    ...(typeof generated.startedAt === "string"
      ? { startedAt: generated.startedAt }
      : {}),
    updatedAt: generated.updatedAt,
    durationSeconds: generated.durationSeconds,
    nextActions: [...generated.nextActions],
    initiator,
    agent,
    role,
    model,
    provider,
  };
}

function mapIncident(value: unknown): IncidentView | null {
  if (
    !isRecord(value) ||
    typeof value.incidentRef !== "string" ||
    !isVersion(value.version) ||
    typeof value.diagnosticSummary !== "string" ||
    typeof value.safeCorrelation !== "string" ||
    typeof value.runbookUrl !== "string" ||
    typeof value.occurredAt !== "string" ||
    typeof value.updatedAt !== "string" ||
    !Array.isArray(value.nextActions)
  )
    return null;
  const workspace = mapDisplay(value.workspace);
  const run = mapDisplay(value.run);
  if (!workspace || !run) return null;
  const generated = value as unknown as IncidentProjection;
  return {
    incidentRef: generated.incidentRef,
    version: generated.version,
    kind: generated.kind,
    state: generated.state,
    severity: generated.severity,
    impact: generated.impact,
    workspace,
    run,
    safeCorrelation: generated.safeCorrelation,
    diagnosticSummary: generated.diagnosticSummary,
    runbookUrl: generated.runbookUrl,
    occurredAt: generated.occurredAt,
    updatedAt: generated.updatedAt,
    nextActions: [...generated.nextActions],
    executionFence: generated.executionFence,
  };
}

function mapTeam(value: unknown): MattermostTeam | null {
  if (
    !isRecord(value) ||
    typeof value.selector !== "string" ||
    typeof value.displayName !== "string" ||
    !["ACTIVE", "DELETED"].includes(String(value.status))
  )
    return null;
  return {
    selector: value.selector,
    displayName: value.displayName,
    slug: String(value.slug),
    status: value.status as MattermostTeam["status"],
    providerSnapshotSha256: String(value.providerSnapshotSha256),
    createdAt: String(value.createdAt),
    updatedAt: String(value.updatedAt),
    observedAt: String(value.observedAt),
  };
}

function mapConnection(value: unknown): ProviderConnection | null {
  if (
    !isRecord(value) ||
    typeof value.connectionRef !== "string" ||
    typeof value.displayName !== "string" ||
    !isVersion(value.version) ||
    !isVersion(value.generation) ||
    typeof value.stableKey !== "string" ||
    typeof value.providerRef !== "string" ||
    !["PENDING", "VALID", "INVALID", "REVOKED"].includes(String(value.state)) ||
    typeof value.maskedLabel !== "string" ||
    typeof value.maskedAccount !== "string" ||
    !Array.isArray(value.capabilities) ||
    !value.capabilities.every((item) => typeof item === "string") ||
    typeof value.capabilityDigestSha256 !== "string" ||
    typeof value.observationDigestSha256 !== "string" ||
    typeof value.observedAt !== "string" ||
    typeof value.updatedAt !== "string" ||
    !isVersion(value.activeCredentialGeneration)
  )
    return null;
  const generated = value as unknown as ProviderConnectionProjection;
  return {
    connectionRef: generated.connectionRef,
    stableKey: generated.stableKey,
    providerRef: generated.providerRef,
    displayName: generated.displayName,
    version: generated.version,
    generation: generated.generation,
    state: generated.state,
    maskedLabel: generated.maskedLabel,
    maskedAccount: generated.maskedAccount,
    capabilities: [...generated.capabilities],
    capabilityDigestSha256: generated.capabilityDigestSha256,
    observationDigestSha256: generated.observationDigestSha256,
    observedAt: generated.observedAt,
    updatedAt: generated.updatedAt,
    activeCredentialGeneration: generated.activeCredentialGeneration,
    ...(generated.capacity
      ? {
          capacity: {
            usage: generated.capacity.usage,
            limit: generated.capacity.limit,
            revision: generated.capacity.revision,
            observedAt: generated.capacity.observedAt,
            windowDurationSeconds: generated.capacity.windowDurationSeconds,
            ...(generated.capacity.resetsAt
              ? { resetsAt: generated.capacity.resetsAt }
              : {}),
            expiresAt: generated.capacity.expiresAt,
            digestSha256: generated.capacity.digestSha256,
          },
        }
      : {}),
  };
}

function mapIntegration(value: unknown): IntegrationConfiguration | null {
  if (
    !isRecord(value) ||
    typeof value.configurationRef !== "string" ||
    typeof value.stableKey !== "string" ||
    !isVersion(value.version) ||
    typeof value.digestSha256 !== "string" ||
    typeof value.definitionRef !== "string" ||
    !isVersion(value.definitionVersion) ||
    typeof value.definitionDigestSha256 !== "string" ||
    typeof value.connectionRef !== "string" ||
    !isVersion(value.connectionVersion) ||
    !isVersion(value.connectionGeneration) ||
    !Array.isArray(value.capabilities) ||
    typeof value.capabilityDigestSha256 !== "string" ||
    !["MCP_TOOL", "CLI", "ENVIRONMENT"].includes(String(value.effectKind)) ||
    !["ACTIVE", "ARCHIVED"].includes(String(value.state)) ||
    typeof value.updatedAt !== "string"
  )
    return null;
  const generated = value as unknown as IntegrationConfigurationProjection;
  return {
    configurationRef: generated.configurationRef,
    stableKey: generated.stableKey,
    version: generated.version,
    digestSha256: generated.digestSha256,
    definitionRef: generated.definitionRef,
    definitionVersion: generated.definitionVersion,
    definitionDigestSha256: generated.definitionDigestSha256,
    connectionRef: generated.connectionRef,
    connectionVersion: generated.connectionVersion,
    connectionGeneration: generated.connectionGeneration,
    capabilities: [...generated.capabilities],
    capabilityDigestSha256: generated.capabilityDigestSha256,
    effectKind: generated.effectKind,
    state: generated.state,
    updatedAt: generated.updatedAt,
  };
}

function mapApproval(value: unknown): IntegrationApproval | null {
  if (
    !isRecord(value) ||
    typeof value.approvalRef !== "string" ||
    !isVersion(value.version) ||
    typeof value.invocationRef !== "string" ||
    !["PENDING", "APPROVED", "REJECTED", "EXPIRED", "CANCELLED"].includes(
      String(value.status),
    ) ||
    typeof value.requestHash !== "string" ||
    !isRecord(value.redactedPreview) ||
    typeof value.redactedPreview.summary !== "string" ||
    !Array.isArray(value.redactedPreview.fields) ||
    !value.redactedPreview.fields.every((item) => typeof item === "string") ||
    typeof value.expiresAt !== "string"
  )
    return null;
  return {
    approvalRef: value.approvalRef,
    invocationRef: value.invocationRef,
    version: value.version,
    status: value.status as IntegrationApproval["status"],
    requestHash: value.requestHash,
    redactedPreview: {
      summary: value.redactedPreview.summary,
      fields: [...value.redactedPreview.fields],
    },
    expiresAt: value.expiresAt,
    ...(typeof value.decidedAt === "string"
      ? { decidedAt: value.decidedAt }
      : {}),
    ...(typeof value.reasonCode === "string"
      ? { reasonCode: value.reasonCode }
      : {}),
  };
}

function mapHealth(value: unknown): HealthObservation | null {
  if (
    !isRecord(value) ||
    typeof value.source !== "string" ||
    typeof value.component !== "string" ||
    !isVersion(value.version) ||
    !["CONTROL_PLANE", "INTERACTION_GATEWAY", "INTEGRATION_GATEWAY"].includes(
      value.source,
    ) ||
    !["OK", "DEGRADED", "UNAVAILABLE", "UNKNOWN"].includes(
      String(value.status),
    ) ||
    typeof value.value !== "number" ||
    typeof value.observedAt !== "string"
  )
    return null;
  return {
    source: value.source as HealthObservation["source"],
    component: value.component,
    status: value.status as HealthObservation["status"],
    value: value.value,
    version: value.version,
    ...(typeof value.digestSha256 === "string"
      ? { digestSha256: value.digestSha256 }
      : {}),
    observedAt: value.observedAt,
  };
}

function mapConfigurationChange(value: unknown): ConfigurationChange | null {
  if (
    !isRecord(value) ||
    typeof value.id !== "string" ||
    !configurationActions.includes(
      value.action as ConfigurationChange["action"],
    ) ||
    typeof value.resourceId !== "string" ||
    !resourceKinds.includes(value.resourceKind as ResourceKind) ||
    !isVersion(value.resourceVersion) ||
    value.outcome !== "succeeded" ||
    typeof value.actorId !== "string" ||
    typeof value.correlationId !== "string" ||
    !isVersion(value.policyRevision) ||
    typeof value.occurredAt !== "string"
  )
    return null;
  return {
    id: value.id,
    action: value.action as ConfigurationChange["action"],
    resourceId: value.resourceId,
    resourceKind: value.resourceKind as ResourceKind,
    resourceVersion: value.resourceVersion,
    outcome: value.outcome,
    actorId: value.actorId,
    correlationId: value.correlationId,
    policyRevision: value.policyRevision,
    occurredAt: value.occurredAt,
  };
}

const expectedItems: Record<ProjectionChannel, string> = {
  RUNS: "runs",
  INCIDENTS: "incidents",
  RESOURCES: "resources",
  CONFIGURATION_CHANGES: "configurationChanges",
  WORKSPACE_TEAMS: "teams",
  PROVIDERS: "providerConnections",
  INTEGRATIONS: "integrationConfigurations",
  APPROVALS: "approvals",
  BACKUPS: "resources",
  HEALTH: "health",
};

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
    !isVersion(value.sequence) ||
    typeof value.snapshotId !== "string" ||
    value.complete !== true ||
    typeof value.serverTime !== "string" ||
    !isRecord(value.items)
  )
    return null;
  const channel = value.channel as ProjectionChannel;
  const keys = Object.keys(value.items);
  const expected = expectedItems[channel];
  if (keys.length !== 1 || keys[0] !== expected) return null;
  const list = value.items[expected];
  if (!Array.isArray(list)) return null;
  const items: RealtimeSnapshot["items"] = {};
  if (expected === "runs") {
    const mapped = list.map(mapRun);
    if (mapped.some((item) => item === null)) return null;
    items.runs = mapped as RunView[];
  } else if (expected === "resources") {
    const mapped = list.map(mapResource);
    if (mapped.some((item) => item === null)) return null;
    items.resources = mapped as Resource[];
  } else if (expected === "incidents") {
    const mapped = list.map(mapIncident);
    if (mapped.some((item) => item === null)) return null;
    items.incidents = mapped as IncidentView[];
  } else if (expected === "configurationChanges") {
    const mapped = list.map(mapConfigurationChange);
    if (mapped.some((item) => item === null)) return null;
    items.configurationChanges = mapped as ConfigurationChange[];
  } else if (expected === "teams") {
    const mapped = list.map(mapTeam);
    if (mapped.some((item) => item === null)) return null;
    items.teams = mapped as MattermostTeam[];
  } else if (expected === "providerConnections") {
    const mapped = list.map(mapConnection);
    if (mapped.some((item) => item === null)) return null;
    items.providerConnections = mapped as ProviderConnection[];
  } else if (expected === "integrationConfigurations") {
    const mapped = list.map(mapIntegration);
    if (mapped.some((item) => item === null)) return null;
    items.integrationConfigurations = mapped as IntegrationConfiguration[];
  } else if (expected === "approvals") {
    const mapped = list.map(mapApproval);
    if (mapped.some((item) => item === null)) return null;
    items.approvals = mapped as IntegrationApproval[];
  } else {
    const mapped = list.map(mapHealth);
    if (mapped.some((item) => item === null)) return null;
    items.health = mapped as HealthObservation[];
  }
  return {
    type: "snapshot",
    snapshot: {
      type: "SNAPSHOT",
      requestId: value.requestId,
      channel,
      sequence: value.sequence,
      snapshotId: value.snapshotId,
      complete: true,
      serverTime: value.serverTime,
      items,
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
    if (this.stopped || !navigator.onLine) {
      this.scheduleReconnect();
      return;
    }
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
