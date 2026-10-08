import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ManagedConfiguration,
  ManagedConfigurationConsumerInput,
  ManagedConfigurationRebindInput,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { history, impact, rebind } from "./api";

export interface IntegrationConnectionBindingPlan {
  configuration: ManagedConfiguration;
  revision: ManagedConfigurationRevision;
  connectionRef: string;
  definitionKey: string;
  input: ManagedConfigurationRebindInput;
  signal: AbortSignal;
}

function positiveVersion(version: number): boolean {
  return Number.isSafeInteger(version) && version > 0;
}

export async function prepareIntegrationConnectionBinding(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
  connectionRef: string,
  definitionKey: string,
  signal: AbortSignal,
): Promise<IntegrationConnectionBindingPlan> {
  signal = requestSignal(signal);
  signal.throwIfAborted();
  const readback = await history(configuration.ref, signal, undefined, 100);
  const fresh = readback.configuration;
  signal.throwIfAborted();
  if (
    fresh.ref !== configuration.ref ||
    fresh.kind !== "INTEGRATION_DEFINITION" ||
    fresh.projectRef ||
    fresh.managedBy !== "UI" ||
    fresh.archived ||
    !positiveVersion(fresh.version) ||
    fresh.version !== configuration.version ||
    revision.state !== "PUBLISHED" ||
    !revision.ref ||
    !connectionRef ||
    !definitionKey
  )
    throw new Error("Integration binding target changed");

  const freshRevision =
    readback.items.find((item) => item.ref === revision.ref) ??
    (fresh.currentRevision?.ref === revision.ref
      ? fresh.currentRevision
      : undefined);
  if (
    !freshRevision ||
    freshRevision.state !== "PUBLISHED" ||
    freshRevision.digest !== revision.digest ||
    freshRevision.contentFormat !== revision.contentFormat
  )
    throw new Error("Integration binding revision changed");

  const connection = (
    await unwrap(
      getIntegrationConnection({
        path: { connectionRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (
    connection.ref !== connectionRef ||
    connection.definitionKey !== definitionKey ||
    connection.state === "DELETED" ||
    !positiveVersion(connection.version)
  )
    throw new Error("Integration binding connection mismatch");

  const binding = connection.definitionConfigurationBinding;
  let consumer: ManagedConfigurationConsumerInput;
  if (binding?.state === "ABSENT") {
    if (Object.keys(binding).some((key) => key !== "state"))
      throw new Error("Invalid integration binding absence");
    consumer = {
      kind: "INTEGRATION_CONNECTION",
      ref: connectionRef,
      expectedAbsent: true,
    };
  } else if (binding?.state === "MATCH") {
    if (
      !binding.configurationRef ||
      !binding.revisionRef ||
      !positiveVersion(binding.bindingVersion) ||
      binding.configurationRef === fresh.ref
    )
      throw new Error("Invalid current integration binding");
    consumer = {
      kind: "INTEGRATION_CONNECTION",
      ref: connectionRef,
      revisionRef: binding.revisionRef,
      version: binding.bindingVersion,
      expectedAbsent: false,
    };
  } else throw new Error("Authoritative integration binding is unavailable");

  const projection = await impact(fresh, freshRevision, signal);
  signal.throwIfAborted();
  if (
    projection.configurationRef !== fresh.ref ||
    projection.targetRevisionRef !== freshRevision.ref ||
    !/^[a-f0-9]{64}$/.test(projection.digest) ||
    !Number.isSafeInteger(projection.total) ||
    projection.total < projection.consumers.length ||
    projection.consumers.some(
      (item) =>
        item.kind !== "INTEGRATION_CONNECTION" ||
        !item.ref ||
        !item.revisionRef ||
        !positiveVersion(item.version) ||
        item.ref === connectionRef,
    )
  )
    throw new Error("Integration binding impact mismatch");
  return {
    configuration: fresh,
    revision: freshRevision,
    connectionRef,
    definitionKey,
    input: { impactDigest: projection.digest, consumers: [consumer] },
    signal,
  };
}

export async function bindIntegrationConnection(
  plan: IntegrationConnectionBindingPlan,
  signal: AbortSignal,
) {
  signal = AbortSignal.any([plan.signal, requestSignal(signal)]);
  const fresh = await prepareIntegrationConnectionBinding(
    plan.configuration,
    plan.revision,
    plan.connectionRef,
    plan.definitionKey,
    signal,
  );
  if (JSON.stringify(fresh.input) !== JSON.stringify(plan.input))
    throw new Error("Integration binding changed before confirmation");
  signal.throwIfAborted();
  return rebind(fresh.configuration, fresh.revision, fresh.input);
}
