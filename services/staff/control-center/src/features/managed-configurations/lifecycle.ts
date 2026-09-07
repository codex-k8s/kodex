import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { requestSignal } from "@/shared/api/client";
import {
  canArchiveConfiguration,
  type ConfigurationCopySource,
} from "./copy-source";

export async function copyConfiguration(
  source: ConfigurationCopySource,
  name: string,
  signal: AbortSignal,
) {
  const request = AbortSignal.any([signal, requestSignal()]);
  const result = await mutate(
    (headers) =>
      source.kind === "ROLE_IMAGE"
        ? sdk.copyRoleImageConfiguration({
            body: {
              name,
              projectRef: source.projectRef,
              ...(source.recipeRef !== undefined
                ? { recipeRef: source.recipeRef }
                : { configurationRef: source.configurationRef }),
            },
            headers: { ...headers, "If-Match": etag(source.version) },
            signal: request,
          })
        : sdk.copyIntegrationDefinitionConfiguration({
            body: {
              name,
              ...(source.shipped
                ? { shipped: source.shipped }
                : { configurationRef: source.configurationRef }),
            },
            headers: { ...headers, "If-Match": etag(source.version) },
            signal: request,
          }),
    source.version,
  );
  const { configuration, revision } = result.data;
  if (
    configuration.kind !== source.kind ||
    configuration.managedBy !== "UI" ||
    configuration.archived ||
    configuration.name !== name ||
    revision.state !== "DRAFT" ||
    configuration.ref === source.configurationRef ||
    !configuration.copyProvenance ||
    configuration.copyProvenance.origin !== source.origin ||
    configuration.copyProvenance.sourceRef !== source.ref ||
    configuration.copyProvenance.sourceRevision !== source.revision ||
    configuration.copyProvenance.sourceVersion !== source.version ||
    !configuration.copyProvenance.sourceDigest ||
    (source.digest &&
      configuration.copyProvenance.sourceDigest !== source.digest) ||
    result.etag !== etag(configuration.version) ||
    (source.kind === "ROLE_IMAGE" &&
      configuration.projectRef !== source.projectRef)
  )
    throw new Error("Configuration copy receipt mismatch");
  return result.data;
}

export async function archiveConfiguration(
  configuration: ManagedConfiguration,
  signal: AbortSignal,
) {
  if (!canArchiveConfiguration(configuration))
    throw new Error("Configuration archive is not allowed");
  const request = AbortSignal.any([signal, requestSignal()]);
  const operation =
    configuration.kind === "ROLE_IMAGE"
      ? sdk.archiveRoleImageConfiguration
      : sdk.archiveIntegrationDefinitionConfiguration;
  const result = await mutate(
    (headers) =>
      operation({
        path: { configurationRef: configuration.ref },
        headers: { ...headers, "If-Match": etag(configuration.version) },
        signal: request,
      }),
    configuration.version,
  );
  const archived = result.data.configuration;
  if (
    archived.ref !== configuration.ref ||
    archived.kind !== configuration.kind ||
    archived.managedBy !== "UI" ||
    !archived.archived ||
    archived.version <= configuration.version ||
    result.etag !== etag(archived.version)
  )
    throw new Error("Configuration archive receipt mismatch");
  const readback = (
    await unwrap(
      sdk.listManagedConfigurationHistory({
        path: { configurationRef: configuration.ref },
        query: { pageSize: 30 },
        signal: request,
      }),
    )
  ).data;
  if (
    readback.configuration.ref !== archived.ref ||
    readback.configuration.kind !== archived.kind ||
    !readback.configuration.archived ||
    readback.configuration.version < archived.version
  )
    throw new Error("Configuration archive readback mismatch");
  return { configuration: readback.configuration };
}
