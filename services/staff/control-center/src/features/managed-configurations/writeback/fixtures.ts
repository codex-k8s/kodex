import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
import { contentDigest, type Proposal } from "./model";

export async function writeBackFixture() {
  const baseContent = "key: old\n";
  const proposedContent = "key: new\n";
  const proposal: Proposal = {
    ref: "proposal_fixture",
    configurationRef: "configuration_fixture",
    sourceRef: "source_fixture",
    connectionRef: "connection_fixture",
    version: 1,
    configurationVersion: 8,
    sourceVersion: 4,
    connectionVersion: 5,
    kind: "ROLE_IMAGE",
    repositoryRef: "owner/repository",
    sourceRefName: "main",
    path: "role.yaml",
    baseCommitSha: "a".repeat(40),
    baseContentSha256: await contentDigest(baseContent),
    proposedContentSha256: await contentDigest(proposedContent),
    approvalDigest: "b".repeat(64),
    contentFormat: "YAML",
    proposalBranch: "kodex/proposal-fixture",
    state: "WAITING_APPROVAL",
    createdAt: "2026-09-06T00:00:00Z",
    expiresAt: "2099-09-06T00:00:00Z",
    nextActions: [
      { action: "APPROVE", enabled: true, reason: "NONE" },
      { action: "REJECT", enabled: true, reason: "NONE" },
      { action: "CANCEL", enabled: true, reason: "NONE" },
    ],
  };
  const configuration: ManagedConfiguration = {
    ref: proposal.configurationRef,
    version: 8,
    kind: "ROLE_IMAGE",
    name: "Fixture",
    managedBy: "GIT",
    source: proposal.sourceRef,
    sourceRevision: proposal.baseCommitSha,
    updatedAt: proposal.createdAt,
    gitSource: {
      ref: proposal.sourceRef,
      version: 4,
      generation: 1,
      connectionRef: proposal.connectionRef,
      providerKey: "github",
      repositoryRef: proposal.repositoryRef,
      refName: "main",
      path: "role.yaml",
      state: "READY",
      acceptedCommitSha: proposal.baseCommitSha,
      acceptedContentSha256: proposal.baseContentSha256,
      acceptedRevisionRef: "revision_fixture",
      syncedAt: proposal.createdAt,
    },
  };
  return {
    configuration,
    proposal,
    view: { proposal, baseContent, proposedContent },
  };
}
export function memoryStorage() {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => {
      data.set(key, value);
    },
    removeItem: (key: string) => {
      data.delete(key);
    },
  };
}
