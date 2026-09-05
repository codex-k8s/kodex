import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ManagedConfiguration,
  ManagedConfigurationDraftInput,
  ManagedConfigurationRebindInput,
  ManagedConfigurationRevision,
  ManagedConfigurationDraftSaveInput,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, etag } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { requestSignal } from "@/shared/api/client";
import { canChangeDraft } from "./model";

export type ConfigurationKind = ManagedConfiguration["kind"];
export async function listConfigurations(options: {
  kind: ConfigurationKind;
  query: string;
  projectRef?: string;
  pageToken?: string;
  signal: AbortSignal;
}) {
  const { signal, ...query } = options;
  return (
    await unwrap(
      sdk.listManagedConfigurations({
        query: { ...query, pageSize: 30 },
        signal: AbortSignal.any([signal, requestSignal()]),
      }),
    )
  ).data;
}
export async function providerAccounts(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      sdk.listProviderAccounts({
        query: { query, pageToken, pageSize: 30 },
        signal: AbortSignal.any([signal, requestSignal()]),
      }),
    )
  ).data;
}
export async function providerAccount(
  providerAccountRef: string,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      sdk.getProviderAccount({
        path: { providerAccountRef },
        signal: AbortSignal.any([signal, requestSignal()]),
      }),
    )
  ).data;
}
const operations = {
  PROMPT_TEMPLATE: {
    create: sdk.createPromptTemplateDraft,
    save: sdk.savePromptTemplateDraft,
    discard: sdk.discardPromptTemplateDraft,
    validate: sdk.validatePromptTemplateDraft,
    publish: sdk.publishPromptTemplateDraft,
    rebind: sdk.rebindPromptTemplateConsumers,
  },
  ROLE_IMAGE: {
    create: sdk.createRoleImageRevisionDraft,
    save: sdk.saveRoleImageRevisionDraft,
    discard: sdk.discardRoleImageRevisionDraft,
    validate: sdk.validateRoleImageRevisionDraft,
    publish: sdk.publishRoleImageRevisionDraft,
    rebind: sdk.rebindRoleImageConsumers,
  },
  INTEGRATION_DEFINITION: {
    create: sdk.createIntegrationDefinitionDraft,
    save: sdk.saveIntegrationDefinitionDraft,
    discard: sdk.discardIntegrationDefinitionDraft,
    validate: sdk.validateIntegrationDefinitionDraft,
    publish: sdk.publishIntegrationDefinitionDraft,
    rebind: sdk.rebindIntegrationDefinitionConsumers,
  },
  SYSTEM_STT: {
    create: sdk.createSystemSttConfigurationDraft,
    save: sdk.saveSystemSttConfigurationDraft,
    discard: sdk.discardSystemSttConfigurationDraft,
    validate: sdk.validateSystemSttConfigurationDraft,
    publish: sdk.publishSystemSttConfigurationDraft,
    rebind: sdk.rebindSystemSttConsumers,
  },
} as const;

export async function history(
  configurationRef: string,
  signal: AbortSignal,
  pageToken?: string,
) {
  return (
    await unwrap(
      sdk.listManagedConfigurationHistory({
        path: { configurationRef },
        query: { pageSize: 30, pageToken },
        signal: AbortSignal.any([requestSignal(), signal]),
      }),
    )
  ).data;
}
export async function impact(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
  signal: AbortSignal,
  query = "",
  pageToken?: string,
) {
  return (
    await unwrap(
      sdk.getManagedConfigurationImpact({
        path: {
          configurationRef: configuration.ref,
          revisionRef: revision.ref,
        },
        query: {
          pageSize: 40,
          ...(pageToken ? { pageToken } : {}),
          ...(query.trim() ? { query: query.trim() } : {}),
        },
        signal: AbortSignal.any([requestSignal(), signal]),
      }),
    )
  ).data;
}
export async function createDraft(
  kind: ConfigurationKind,
  body: ManagedConfigurationDraftInput,
  version?: number,
) {
  return (
    await mutate(
      (headers) =>
        operations[kind].create({
          body,
          headers: { ...headers },
          signal: requestSignal(),
        }),
      version,
    )
  ).data;
}
export async function changeDraft(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
  body?: ManagedConfigurationDraftSaveInput,
) {
  if (!canChangeDraft(configuration, revision))
    throw new Error("Managed draft is immutable");
  if (
    body &&
    (new TextEncoder().encode(body.content).length > 256 * 1024 ||
      body.content.includes("\0") ||
      (configuration.kind === "PROMPT_TEMPLATE"
        ? body.contentFormat !== "TEXT"
        : !["JSON", "YAML", "TOML"].includes(body.contentFormat)))
  )
    throw new Error("Invalid managed draft content");
  const result = await mutate((headers) => {
    const options = {
      path: { configurationRef: configuration.ref, revisionRef: revision.ref },
      headers: { ...headers, "If-Match": etag(configuration.version) },
      signal: requestSignal(),
    };
    return body
      ? operations[configuration.kind].save({ ...options, body })
      : operations[configuration.kind].discard(options);
  }, configuration.version);
  const next = result.data;
  if (
    next.configuration.ref !== configuration.ref ||
    next.configuration.kind !== configuration.kind ||
    next.configuration.managedBy !== "UI" ||
    next.configuration.version !== configuration.version + 1 ||
    result.etag !== etag(next.configuration.version) ||
    (body
      ? next.revision.ref === revision.ref ||
        next.revision.parentRevisionRef !== revision.ref ||
        next.revision.state !== "DRAFT" ||
        next.revision.revision <= revision.revision ||
        next.revision.contentFormat !== body.contentFormat
      : next.revision.ref !== revision.ref ||
        next.revision.state !== "DISCARDED" ||
        next.revision.revision !== revision.revision)
  )
    throw new Error("Managed draft receipt mismatch");
  return next;
}
export async function transition(
  action: "validate" | "publish",
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
) {
  return (
    await mutate(
      (headers) =>
        operations[configuration.kind][action]({
          path: {
            configurationRef: configuration.ref,
            revisionRef: revision.ref,
          },
          headers: { ...headers, "If-Match": etag(configuration.version) },
          signal: requestSignal(),
        }),
      configuration.version,
    )
  ).data;
}
export async function rebind(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
  body: ManagedConfigurationRebindInput,
) {
  return (
    await mutate(
      (headers) =>
        operations[configuration.kind].rebind({
          path: {
            configurationRef: configuration.ref,
            revisionRef: revision.ref,
          },
          body,
          headers: { ...headers, "If-Match": etag(configuration.version) },
          signal: requestSignal(),
        }),
      configuration.version,
    )
  ).data;
}
export async function detach(configuration: ManagedConfiguration) {
  return (
    await mutate(
      (headers) =>
        sdk.detachGitManagedConfiguration({
          path: { configurationRef: configuration.ref },
          headers: { ...headers, "If-Match": etag(configuration.version) },
          signal: requestSignal(),
        }),
      configuration.version,
    )
  ).data;
}
export async function copy(configuration: ManagedConfiguration, name: string) {
  return (
    await mutate(
      (headers) =>
        sdk.copyGitManagedConfiguration({
          path: { configurationRef: configuration.ref },
          body: { name },
          headers: { ...headers, "If-Match": etag(configuration.version) },
          signal: requestSignal(),
        }),
      configuration.version,
    )
  ).data;
}
