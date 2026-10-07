import { capabilityCandidates } from "@/features/integrations/grant-candidates";
import { requestSignal } from "@/shared/api/client";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import {
  getAgent,
  getIntegrationConnection,
  getWorkflow,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  Workflow,
  IntegrationConnection,
  IntegrationGrantCapabilityCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export interface GrantReadAddress {
  projectRef: string;
  connectionRef: string;
  recipientKind: "AGENT" | "WORKFLOW";
  recipientRef: string;
  connectionVersion: number;
}
export interface GrantReadSnapshot {
  connection: IntegrationConnection;
  recipient: Agent | Workflow;
  candidates: IntegrationGrantCapabilityCandidate[];
}

// Один экземпляр принадлежит только одной ревизии открытого плана.
export function createIntegrationGrantReadBundle() {
  const controller = new AbortController();
  const lifetime = AbortSignal.any([controller.signal, ownerRequestSignal()]);
  const reads = new Map<string, Promise<GrantReadSnapshot>>();
  async function load(address: GrantReadAddress): Promise<GrantReadSnapshot> {
    lifetime.throwIfAborted();
    const {
      projectRef,
      connectionRef,
      recipientKind,
      recipientRef,
      connectionVersion,
    } = address;
    if (
      !projectRef ||
      !connectionRef ||
      !recipientRef ||
      !["AGENT", "WORKFLOW"].includes(recipientKind) ||
      !Number.isSafeInteger(connectionVersion) ||
      connectionVersion < 1
    )
      throw new Error("Invalid assistant grant read address");
    const signal = requestSignal(lifetime);
    const [connectionResponse, recipientResponse] = await Promise.all([
      unwrap(getIntegrationConnection({ path: { connectionRef }, signal })),
      recipientKind === "AGENT"
        ? unwrap(getAgent({ path: { agentRef: recipientRef }, signal }))
        : unwrap(getWorkflow({ path: { workflowRef: recipientRef }, signal })),
    ]);
    lifetime.throwIfAborted();
    const connection = connectionResponse.data;
    const recipient = recipientResponse.data;
    if (
      connection.ref !== connectionRef ||
      recipient.ref !== recipientRef ||
      recipient.projectRef !== projectRef
    )
      throw new Error("Assistant integration grant readback mismatch");
    if (connection.version !== connectionVersion)
      return { connection, recipient, candidates: [] };
    const pageLoader = capabilityCandidates({
      projectRef,
      connectionRef,
      recipientKind,
      recipientRef,
    });
    const candidates: IntegrationGrantCapabilityCandidate[] = [];
    const keys = new Set<string>();
    const cursors = new Set<string>();
    let cursor: string | undefined;
    let total: number | undefined;
    for (let pageCount = 0; pageCount < 10; pageCount++) {
      lifetime.throwIfAborted();
      const page = await pageLoader("", cursor, lifetime, 100);
      lifetime.throwIfAborted();
      if (
        page.pins.connectionVersion !== connectionVersion ||
        (total !== undefined && total !== page.total) ||
        page.total > 1000
      )
        throw new Error("Assistant integration candidate catalog changed");
      total = page.total;
      for (const candidate of page.items) {
        if (keys.has(candidate.capability.key))
          throw new Error("Duplicate assistant integration candidate");
        keys.add(candidate.capability.key);
        candidates.push(candidate);
      }
      if (!page.nextPageToken) {
        if (candidates.length !== total)
          throw new Error("Incomplete assistant integration candidate catalog");
        return { connection, recipient, candidates };
      }
      if (cursors.has(page.nextPageToken))
        throw new Error("Repeated assistant integration candidate cursor");
      cursors.add(page.nextPageToken);
      cursor = page.nextPageToken;
    }
    throw new Error("Assistant integration candidate catalog exceeds bound");
  }
  return {
    read(
      address: GrantReadAddress,
      subscriber: AbortSignal,
    ): Promise<GrantReadSnapshot> {
      lifetime.throwIfAborted();
      subscriber.throwIfAborted();
      const key = JSON.stringify([
        address.projectRef,
        address.connectionRef,
        address.recipientKind,
        address.recipientRef,
        address.connectionVersion,
      ]);
      let pending = reads.get(key);
      if (!pending) {
        pending = load(address);
        reads.set(key, pending);
      }
      // Закрытие одной строки не отменяет чтение соседних строк того же плана.
      return new Promise((resolve, reject) => {
        const signal = AbortSignal.any([subscriber, lifetime]);
        const abort = () =>
          reject(
            new DOMException("Assistant grant read aborted", "AbortError"),
          );
        signal.addEventListener("abort", abort, { once: true });
        pending.then(
          (snapshot) => {
            signal.removeEventListener("abort", abort);
            if (signal.aborted) {
              abort();
              return;
            }
            resolve(snapshot);
          },
          (error: unknown) => {
            signal.removeEventListener("abort", abort);
            reject(
              error instanceof Error
                ? error
                : new Error("Assistant grant read failed"),
            );
          },
        );
      });
    },
    close() {
      controller.abort();
      reads.clear();
    },
  };
}
export type IntegrationGrantReadBundle = ReturnType<
  typeof createIntegrationGrantReadBundle
>;
