import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate, mutateWithRetry } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { checkMutationRejection } from "@/shared/api/mutation-rejection";
export { KnownMutationRejection as PreparationRejected } from "@/shared/api/mutation-rejection";
import {
  checkedIntent,
  checkedPage,
  checkedProposal,
  checkedView,
  contentDigest,
  type Intent,
  type Proposal,
} from "./model";

export async function readProposal(
  configurationRef: string,
  proposalRef: string,
  signal: AbortSignal,
  previous?: Proposal,
) {
  const view = (
    await unwrap(
      sdk.getManagedConfigurationGitWriteBack({
        path: { proposalRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (view.proposal.ref !== proposalRef)
    throw new Error("Write-back proposal scope mismatch");
  const result = await checkedView(view, configurationRef, previous);
  signal.throwIfAborted();
  return result;
}
export async function listProposals(
  configurationRef: string,
  signal: AbortSignal,
  pageToken?: string,
  previous: Proposal[] = [],
) {
  const page = (
    await unwrap(
      sdk.listManagedConfigurationGitWriteBacks({
        path: { configurationRef },
        query: { pageSize: 30, pageToken },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  signal.throwIfAborted();
  return checkedPage(page, configurationRef, pageToken, previous);
}
export async function executeIntent(
  intent: Intent,
  signal: AbortSignal,
  content?: string,
): Promise<Proposal> {
  checkedIntent(intent);
  signal.throwIfAborted();
  if (
    intent.action === "PREPARE" &&
    (content === undefined ||
      (await contentDigest(content)) !== intent.contentDigest)
  )
    throw new Error("Write-back recovery content differs from original intent");
  signal.throwIfAborted();
  // Prepare имеет один transport attempt: окончательный typed rejection нельзя
  // смешивать с неявным повтором после потерянного ACK. Повтор выбирает человек.
  const send = intent.action === "PREPARE" ? mutate : mutateWithRetry;
  const response = await send(
    async (headers) => {
      signal.throwIfAborted();
      const options = {
        headers: { ...headers, "If-Match": etag(intent.version) },
        signal: requestSignal(signal),
      };
      if (intent.action === "PREPARE") {
        if (intent.sourceVersion === undefined || content === undefined)
          throw new Error("Write-back preparation is incomplete");
        const request = {
          ...options,
          path: { configurationRef: intent.configurationRef },
          body: { expectedSourceVersion: intent.sourceVersion, content },
        };
        const result = await (intent.kind === "ROLE_IMAGE"
          ? sdk.prepareRoleImageGitWriteBack(request)
          : sdk.prepareIntegrationDefinitionGitWriteBack(request));
        return checkMutationRejection(result);
      }
      if (!intent.proposalRef || !intent.approvalDigest)
        throw new Error("Write-back decision is incomplete");
      const request = {
        ...options,
        path: { proposalRef: intent.proposalRef },
        body: { approvalDigest: intent.approvalDigest },
      };
      switch (intent.action) {
        case "APPROVE":
          return sdk.approveManagedConfigurationGitWriteBack(request);
        case "REJECT":
          return sdk.rejectManagedConfigurationGitWriteBack(request);
        case "CANCEL":
          return sdk.cancelManagedConfigurationGitWriteBack({
            ...options,
            path: request.path,
          });
      }
    },
    intent.version,
    intent.key,
  );
  signal.throwIfAborted();
  const proposal = checkedProposal(response.data, intent.configurationRef);
  if (
    proposal.kind !== intent.kind ||
    (intent.action === "PREPARE"
      ? proposal.configurationVersion !== intent.version ||
        proposal.sourceVersion !== intent.sourceVersion ||
        proposal.sourceRef !== intent.sourceRef ||
        proposal.proposedContentSha256 !== intent.contentDigest
      : proposal.ref !== intent.proposalRef ||
        proposal.approvalDigest !== intent.approvalDigest ||
        proposal.version <= intent.version)
  )
    throw new Error("Write-back mutation receipt mismatch");
  return proposal;
}
