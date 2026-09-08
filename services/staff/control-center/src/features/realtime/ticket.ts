import { requestSignal } from "@/shared/api/client";
import { createOwnerSessionTicket } from "@/shared/api/generated/openapi/sdk.gen";
import { csrfToken } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export async function requestRealtimeTicket(
  signal: AbortSignal,
): Promise<string> {
  const response = await unwrap(
    createOwnerSessionTicket({
      headers: { "X-CSRF-Token": csrfToken() },
      signal: requestSignal(signal),
    }),
  );
  signal.throwIfAborted();
  if (
    !/^[A-Za-z0-9_-]{43}$/.test(response.data.ticket) ||
    !Number.isFinite(Date.parse(response.data.expiresAt))
  )
    throw new Error("WebSocket ticket response is invalid");
  return response.data.ticket;
}
