import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  EmailMailboxConfigurationView,
  EmailMailboxDraftContent,
  EmailMailboxDraftInput,
  EmailMailboxCredentialKind,
  EmailMailboxConfigurationPage,
  EmailMailboxCredentialPage,
} from "@/shared/api/generated/openapi/types.gen";
import {
  csrfToken,
  etag,
  mutate,
  type MutationHeaders,
} from "@/shared/api/mutation";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

export type {
  EmailMailboxConfigurationView,
  EmailMailboxDraftContent,
  EmailMailboxDraftInput,
};
const positive = (value: number) => Number.isSafeInteger(value) && value > 0;
function headers(value: MutationHeaders) {
  if (!value["If-Match"])
    throw new Error("Mailbox configuration version is unavailable");
  return { ...value, "If-Match": value["If-Match"] };
}
export function checkedMailbox(
  view: EmailMailboxConfigurationView,
  connectionRef: string,
  configurationRef?: string,
  revisionRef?: string,
): EmailMailboxConfigurationView {
  if (
    view.connectionRef !== connectionRef ||
    !positive(view.connectionVersion) ||
    !view.mailboxRef ||
    !view.configuration.ref ||
    view.configuration.kind !== "EMAIL_MAILBOX" ||
    !positive(view.configuration.version) ||
    !["UI", "GIT"].includes(view.configuration.managedBy) ||
    (configurationRef && view.configuration.ref !== configurationRef) ||
    !view.revision.ref ||
    !positive(view.revision.revision) ||
    (revisionRef && view.revision.ref !== revisionRef) ||
    ![
      "DRAFT",
      "VALID",
      "INVALID",
      "PUBLISHED",
      "SUPERSEDED",
      "DISCARDED",
    ].includes(view.revision.state) ||
    typeof view.specification !== "object" ||
    !Array.isArray(view.diagnostics) ||
    (view.publication &&
      (!positive(view.publication.revision) ||
        !view.publication.ref ||
        !view.publication.digest ||
        !["PENDING", "READY", "FAILED", "SUPERSEDED"].includes(
          view.publication.state,
        )))
  )
    throw new Error("Mailbox configuration receipt mismatch");
  return view;
}

export async function readMailbox(
  connectionRef: string,
  signal: AbortSignal,
  configurationRef?: string,
  revisionRef?: string,
): Promise<EmailMailboxConfigurationView> {
  const result = (
    await unwrap(
      sdk.getEmailMailboxConfiguration({
        path: { connectionRef },
        query: {
          ...(configurationRef ? { configurationRef } : {}),
          ...(revisionRef ? { revisionRef } : {}),
        },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  return checkedMailbox(result, connectionRef, configurationRef, revisionRef);
}
export async function listMailboxes(
  connectionRef: string,
  query: string,
  signal: AbortSignal,
  pageToken?: string,
): Promise<EmailMailboxConfigurationPage> {
  const result = (
    await unwrap(
      sdk.listEmailMailboxConfigurations({
        path: { connectionRef },
        query: {
          pageSize: 30,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    !Array.isArray(result.items) ||
    !Number.isSafeInteger(result.total) ||
    result.total < result.items.length ||
    typeof result.nextPageToken !== "string" ||
    (pageToken && result.nextPageToken === pageToken)
  )
    throw new Error("Mailbox configuration page is invalid");
  result.items = result.items.map((view) =>
    checkedMailbox(view, connectionRef),
  );
  if (
    new Set(result.items.map((view) => view.configuration.ref)).size !==
    result.items.length
  )
    throw new Error("Mailbox configuration page contains duplicates");
  return result;
}
export async function listMailboxCredentials(
  connectionRef: string,
  kind: EmailMailboxCredentialKind,
  signal: AbortSignal,
  pageToken?: string,
): Promise<EmailMailboxCredentialPage> {
  const result = (
    await unwrap(
      sdk.listEmailMailboxCredentials({
        path: { connectionRef },
        query: { kind, pageSize: 40, ...(pageToken ? { pageToken } : {}) },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (
    !Array.isArray(result.items) ||
    !Number.isSafeInteger(result.total) ||
    result.total < result.items.length ||
    typeof result.nextPageToken !== "string" ||
    (pageToken && result.nextPageToken === pageToken) ||
    result.items.some(
      (item) =>
        item.connectionRef !== connectionRef ||
        item.kind !== kind ||
        !item.name ||
        !positive(item.generation) ||
        !positive(item.connectionVersion),
    ) ||
    new Set(
      result.items.map((item) => JSON.stringify([item.name, item.generation])),
    ).size !== result.items.length
  )
    throw new Error("Mailbox credential page is invalid");
  return result;
}
export async function previewMailbox(
  connectionRef: string,
  content: EmailMailboxDraftContent,
  signal: AbortSignal,
) {
  const result = (
    await unwrap(
      sdk.previewEmailMailboxConfiguration({
        path: { connectionRef },
        body: content,
        headers: { "X-CSRF-Token": csrfToken() },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    !Array.isArray(result.diagnostics) ||
    typeof result.canonicalYaml !== "string" ||
    typeof result.valid !== "boolean" ||
    (result.valid && !result.specification) ||
    (result.diagnostics.some(
      (item) => item.code === "EMAIL_MAILBOX_SYNTAX_INVALID",
    ) &&
      result.specification)
  )
    throw new Error("Mailbox preview is invalid");
  return result;
}
export async function createMailboxDraft(
  connectionRef: string,
  input: EmailMailboxDraftInput,
  key: string,
  version?: number,
) {
  if (Boolean(input.configurationRef) !== (version !== undefined))
    throw new Error("Mailbox draft base version is required");
  const result = await mutate(
    (value) =>
      sdk.createEmailMailboxDraft({
        path: { connectionRef },
        body: input,
        headers: { ...value },
        signal: requestSignal(),
      }),
    version,
    key,
  );
  return checkedMailbox(result.data, connectionRef, input.configurationRef);
}
export async function saveMailboxDraft(
  view: EmailMailboxConfigurationView,
  content: EmailMailboxDraftContent,
  key: string,
) {
  const result = await mutate(
    (value) =>
      sdk.saveEmailMailboxDraft({
        path: {
          configurationRef: view.configuration.ref,
          revisionRef: view.revision.ref,
        },
        body: content,
        headers: headers(value),
        signal: requestSignal(),
      }),
    view.configuration.version,
    key,
  );
  return checkedMailbox(
    result.data,
    view.connectionRef,
    view.configuration.ref,
  );
}
export async function changeMailboxDraft(
  view: EmailMailboxConfigurationView,
  action: "validate" | "publish" | "discard",
  key: string,
) {
  const operation =
    action === "validate"
      ? sdk.validateEmailMailboxDraft
      : action === "publish"
        ? sdk.publishEmailMailboxDraft
        : sdk.discardEmailMailboxDraft;
  const result = await mutate(
    (value) =>
      operation({
        path: {
          configurationRef: view.configuration.ref,
          revisionRef: view.revision.ref,
        },
        headers: headers(value),
        signal: requestSignal(),
      }),
    view.configuration.version,
    key,
  );
  return checkedMailbox(
    result.data,
    view.connectionRef,
    view.configuration.ref,
    view.revision.ref,
  );
}
export async function bindMailbox(
  view: EmailMailboxConfigurationView,
  key: string,
) {
  const result = await mutate(
    (value) =>
      sdk.bindEmailMailboxConfiguration({
        path: {
          configurationRef: view.configuration.ref,
          revisionRef: view.revision.ref,
        },
        body: {
          connectionRef: view.connectionRef,
          expectedConnectionVersion: view.connectionVersion,
        },
        headers: headers(value),
        signal: requestSignal(),
      }),
    view.configuration.version,
    key,
  );
  return checkedMailbox(
    result.data,
    view.connectionRef,
    view.configuration.ref,
    view.revision.ref,
  );
}
export async function unbindMailbox(
  view: EmailMailboxConfigurationView,
  key: string,
) {
  const result = await mutate(
    (value) =>
      sdk.unbindEmailMailboxConfiguration({
        path: { connectionRef: view.connectionRef },
        headers: { ...value, "If-Match": etag(view.connectionVersion) },
        signal: requestSignal(),
      }),
    view.connectionVersion,
    key,
  );
  if (
    !positive(result.data.connectionVersion) ||
    result.data.publication.configurationRevisionRef !== "" ||
    !["PENDING", "READY", "FAILED", "SUPERSEDED"].includes(
      result.data.publication.state,
    )
  )
    throw new Error("Mailbox unbinding receipt mismatch");
  return result.data;
}
