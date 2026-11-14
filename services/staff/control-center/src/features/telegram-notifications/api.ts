import {
  getTelegramSubscription,
  configureTelegramSubscription,
  listInteractionDeliveries,
  listIntegrationConnections,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { TelegramSubscriptionInput } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { mutate } from "@/shared/api/mutation";
import { subscriptionView, deliveryView, type ConnectionChoice } from "./model";

export async function readSubscription(
  projectRef: string,
  signal: AbortSignal,
) {
  const { data } = await unwrap(
    getTelegramSubscription({
      path: { projectRef },
      signal: requestSignal(signal),
    }),
  );
  if (typeof data.canManage !== "boolean")
    throw new Error("Telegram subscription eligibility is invalid");
  const subscription =
    data.subscription === undefined
      ? undefined
      : subscriptionView(data.subscription, projectRef);
  if (subscription && subscription.canManage !== data.canManage)
    throw new Error("Telegram subscription eligibility changed");
  return { subscription, canManage: data.canManage };
}

// Одна попытка команды. Неоднозначная ошибка требует авторитетного чтения, а не продления срока повтором.
export async function saveSubscription(
  projectRef: string,
  input: TelegramSubscriptionInput,
  version: number | undefined,
  signal: AbortSignal,
) {
  const { data } = await mutate(
    (headers) =>
      configureTelegramSubscription({
        path: { projectRef },
        headers: { ...headers },
        body: input,
        signal: requestSignal(signal),
      }),
    version,
  );
  const subscription = subscriptionView(data, projectRef);
  if (
    subscription.enabled !== input.enabled ||
    (input.enabled && subscription.connectionRef !== input.connectionRef) ||
    (version !== undefined && subscription.version !== version + 1)
  )
    throw new Error("Telegram subscription receipt is invalid");
  return subscription;
}

export async function readDeliveries(
  projectRef: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  const { data } = await unwrap(
    listInteractionDeliveries({
      path: { projectRef },
      query: { pageSize: 20, pageToken },
      signal: requestSignal(signal),
    }),
  );
  if (
    !Array.isArray(data.items) ||
    data.items.length > 20 ||
    (data.nextPageToken !== undefined &&
      (typeof data.nextPageToken !== "string" ||
        data.nextPageToken.length > 512))
  )
    throw new Error("Interaction delivery page is invalid");
  const items = data.items.map(deliveryView);
  if (new Set(items.map((item) => item.ref)).size !== items.length)
    throw new Error("Interaction delivery page contains duplicate rows");
  return { items, nextPageToken: data.nextPageToken || undefined };
}

export async function readConnections(
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  const { data } = await unwrap(
    listIntegrationConnections({
      query: { definitionKey: "telegram", pageSize: 20, pageToken },
      signal: requestSignal(signal),
    }),
  );
  if (
    !Array.isArray(data.items) ||
    data.items.length > 20 ||
    typeof data.nextPageToken !== "string" ||
    data.nextPageToken.length > 512
  )
    throw new Error("Telegram connection page is invalid");
  const items: ConnectionChoice[] = [];
  for (const item of data.items) {
    if (
      item.definitionKey !== "telegram" ||
      !item.ref ||
      typeof item.name !== "string" ||
      !Array.isArray(item.nextActions)
    )
      throw new Error("Telegram connection metadata is invalid");
    if (
      item.credentialPolicy === "PROJECT_SHARED" &&
      item.state === "CONNECTED" &&
      item.credentialsConfigured &&
      item.nextActions.includes("EDIT")
    )
      items.push({ ref: item.ref, name: item.name });
  }
  return { items, nextPageToken: data.nextPageToken || undefined };
}
