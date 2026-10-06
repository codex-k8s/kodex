import type {
  AssistantPlanOperationInput,
  IntegrationConnection,
  ProjectAssistantIntegrationGrantCandidates,
} from "@/shared/api/generated/openapi/types.gen";
import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { unwrap } from "@/shared/api/problem";
import { projectIntegrationGrantPlanOwner } from "./project-integration-grant-plan";
import {
  readProjectGrantOwner,
  readProjectGrantCandidates,
} from "./project-integration-grants";

interface ProjectGrantReadSnapshot {
  connection: IntegrationConnection;
  page: ProjectAssistantIntegrationGrantCandidates;
}

// Каталог только PROJECT помощника; один экземпляр на открытую ревизию плана.
export function createProjectIntegrationGrantReadBundle() {
  const controller = new AbortController();
  const lifetime = AbortSignal.any([controller.signal, ownerRequestSignal()]);
  const reads = new Map<string, Promise<ProjectGrantReadSnapshot>>();
  async function load(
    operation: AssistantPlanOperationInput,
    organizationRef: string,
    applied: boolean,
  ): Promise<ProjectGrantReadSnapshot> {
    lifetime.throwIfAborted();
    const parameters = operation.parameters;
    const connectionRef = parameters.connectionRef as string;
    const [connectionResponse, owner] = await Promise.all([
      unwrap(
        getIntegrationConnection({
          path: { connectionRef },
          signal: requestSignal(lifetime),
          cache: "no-store",
        }),
      ),
      readProjectGrantOwner(
        organizationRef,
        parameters.projectRef as string,
        parameters.projectAssistantRef as string,
        lifetime,
      ),
    ]);
    lifetime.throwIfAborted();
    const connection = connectionResponse.data;
    if (
      owner.assistantProfileRef !== parameters.assistantProfileRef ||
      owner.profileVersion !== parameters.profileVersion ||
      owner.assistantVersion !== parameters.agentVersion ||
      connection.ref !== connectionRef ||
      connection.definitionVersion !== parameters.definitionVersion ||
      connection.definitionDigest !== parameters.definitionDigest ||
      (applied
        ? connection.version <
          (operation.expectedVersion ?? Number.POSITIVE_INFINITY)
        : connection.version !== operation.expectedVersion)
    )
      throw new Error("Project assistant grant read pins changed");
    let first: ProjectAssistantIntegrationGrantCandidates | undefined;
    const candidates: ProjectAssistantIntegrationGrantCandidates["items"] = [];
    const keys = new Set<string>();
    const cursors = new Set<string>();
    let cursor: string | undefined;
    for (let pageCount = 0; pageCount < 10; pageCount++) {
      lifetime.throwIfAborted();
      const page = await readProjectGrantCandidates(
        owner,
        connection,
        "",
        cursor,
        lifetime,
        100,
      );
      lifetime.throwIfAborted();
      if (page.total > 1000 || (first && first.total !== page.total))
        throw new Error("Project assistant grant catalog changed");
      first ??= page;
      for (const candidate of page.items) {
        if (keys.has(candidate.capability.key))
          throw new Error("Duplicate project assistant grant candidate");
        keys.add(candidate.capability.key);
        candidates.push(candidate);
      }
      if (!page.nextPageToken) {
        if (candidates.length !== page.total)
          throw new Error("Incomplete project assistant grant catalog");
        return {
          connection,
          page: { ...first, items: candidates, nextPageToken: undefined },
        };
      }
      if (cursors.has(page.nextPageToken))
        throw new Error("Repeated project assistant grant cursor");
      cursors.add(page.nextPageToken);
      cursor = page.nextPageToken;
    }
    throw new Error("Project assistant grant catalog exceeds bound");
  }
  return {
    read(
      operation: AssistantPlanOperationInput,
      organizationRef: string | undefined,
      subscriber: AbortSignal,
      applied = false,
    ): Promise<ProjectGrantReadSnapshot> {
      lifetime.throwIfAborted();
      subscriber.throwIfAborted();
      if (
        !organizationRef ||
        !projectIntegrationGrantPlanOwner(operation, organizationRef)
      )
        return Promise.reject(
          new Error("Invalid project assistant grant plan owner"),
        );
      const parameters = operation.parameters;
      const key = JSON.stringify([
        organizationRef,
        parameters.assistantScope,
        parameters.scopeKind,
        parameters.projectRef,
        parameters.assistantProfileRef,
        parameters.projectAssistantRef,
        parameters.agentVersion,
        parameters.profileVersion,
        parameters.connectionRef,
        operation.expectedVersion,
        parameters.definitionVersion,
        parameters.definitionDigest,
        applied,
      ]);
      let pending = reads.get(key);
      if (!pending) {
        pending = load(operation, organizationRef, applied);
        reads.set(key, pending);
      }
      return new Promise((resolve, reject) => {
        const signal = AbortSignal.any([subscriber, lifetime]);
        const abort = () =>
          reject(
            new DOMException(
              "Project assistant grant read aborted",
              "AbortError",
            ),
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
                : new Error("Project assistant grant read failed"),
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
export type ProjectIntegrationGrantReadBundle = ReturnType<
  typeof createProjectIntegrationGrantReadBundle
>;
