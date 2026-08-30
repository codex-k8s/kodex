import type { Agent } from "@/shared/api/generated/openapi/types.gen";

export type AgentCatalogView = "grid" | "list";
export type AgentStatusTone = "success" | "accent" | "neutral";

export interface AgentCatalogItem {
  ref: string;
  name: string;
  purpose: string;
  role: string;
  roleDescription: string;
  state: Agent["state"];
  statusTone: AgentStatusTone;
  avatarUrl?: string;
  initials: string;
  avatarTone: number;
  runtimeName: string;
  runtimeProvider?: string;
  runtimeModel?: string;
  runtimeRevision?: string;
  runtimeReady: boolean;
  currentActivity?: string;
  updatedAt: string;
}

export function agentInitials(name: string): string {
  const words = name.trim().split(/\s+/u).filter(Boolean);
  if (words.length === 0) return "AI";
  return words
    .slice(0, 2)
    .map((word) => Array.from(word)[0] ?? "")
    .join("")
    .toLocaleUpperCase("ru-RU");
}

export function agentAvatarTone(seed: string): number {
  let hash = 0;
  for (const character of seed)
    hash = (hash * 31 + (character.codePointAt(0) ?? 0)) | 0;
  return Math.abs(hash) % 6;
}

export function sameOriginAvatarUrl(value?: string): string | undefined {
  const source = value?.trim();
  if (!source) return undefined;
  return /^\/api\/v1\/artifacts\/[A-Za-z0-9_-]{8,96}\/content\?purpose=PREVIEW$/.test(
    source,
  )
    ? source
    : undefined;
}

export function agentStatusTone(state: Agent["state"]): AgentStatusTone {
  if (state === "READY") return "success";
  if (state === "RUNNING") return "accent";
  return "neutral";
}

export function toAgentCatalogItem(agent: Agent): AgentCatalogItem {
  return {
    ref: agent.ref,
    name: agent.name,
    purpose: agent.purpose,
    role: agent.roleDefinitionName?.trim() ?? "",
    roleDescription: agent.roleDescription,
    state: agent.state,
    statusTone: agentStatusTone(agent.state),
    avatarUrl: sameOriginAvatarUrl(agent.avatarUrl),
    initials: agentInitials(agent.name),
    avatarTone: agentAvatarTone(agent.ref || agent.name),
    runtimeName: agent.runtimeName,
    runtimeProvider: agent.runtimeProvider,
    runtimeModel: agent.runtimeModel,
    runtimeRevision: agent.runtimeRevision,
    runtimeReady: agent.runtimeReady,
    currentActivity: agent.currentActivity,
    updatedAt: agent.updatedAt,
  };
}

export function parseAgentCatalogView(value: string | null): AgentCatalogView {
  return value === "list" ? "list" : "grid";
}
