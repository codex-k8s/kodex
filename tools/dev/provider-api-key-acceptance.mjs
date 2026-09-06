import { randomUUID } from "node:crypto";
import { createOwnerSessionClient } from "./owner-session-client.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";

const failure =
  "Provider fixture authorization failed; no automatic retry was performed";

// Этот helper не принимает Page/APIRequestContext: credential остаётся в Node.
// Любой ответ/исключение закрыто нормализуется без содержимого запроса.
export async function authorizeProviderAPIKeyFixture({
  origin,
  storage,
  accountRef,
  apiKey,
  fetchAPI = fetch,
  onSessionCookies = async () => {},
}) {
  let client;
  try {
    if (
      !/^pacc_[A-Za-z0-9_-]{1,96}$/.test(accountRef) ||
      typeof apiKey !== "string" ||
      apiKey.length < 8 ||
      apiKey.length > 16384 ||
      apiKey.trim() !== apiKey ||
      /[\r\n\0]/.test(apiKey)
    )
      throw new Error(failure);
    const signal = AbortSignal.timeout(60000);
    client = createOwnerSessionClient({
      origin,
      storage,
      // Ни body, ни cookie с отражённым credential не доходят до consumers.
      fetchAPI: async (url, options) => {
        const response = await fetchAPI(url, options);
        const payload = await boundedResponseBody(response, 262144);
        const body = payload.toString("utf8");
        const reflected = [
          ...response.headers.values(),
          response.statusText,
          body,
        ].some(
          (value) =>
            value.includes(apiKey) ||
            value.includes(JSON.stringify(apiKey).slice(1, -1)),
        );
        if (reflected) {
          throw new Error(failure);
        }
        return new Response(payload, {
          status: response.status,
          headers: response.headers,
        });
      },
    });
    const path = `/api/v1/provider-accounts/${accountRef}`;
    const current = await client.request(path, { signal });
    const etag = current.headers.get("etag");
    if (current.status !== 200 || !/^"[1-9][0-9]*"$/.test(etag ?? "")) {
      await current.body?.cancel().catch(() => {});
      throw new Error(failure);
    }
    const descriptor = JSON.parse(
      (await boundedResponseBody(current, 262144)).toString("utf8"),
    );
    if (
      descriptor.ref !== accountRef ||
      etag !== `"${descriptor.version}"` ||
      !descriptor.nextActions?.includes("CONFIGURE_CREDENTIAL")
    )
      throw new Error(failure);
    const response = await client.request(`${path}/api-key-authorization`, {
      method: "POST",
      signal,
      headers: {
        "Content-Type": "application/json",
        "If-Match": etag,
        "Idempotency-Key": randomUUID(),
      },
      body: JSON.stringify({ apiKey }),
    });
    if (response.status !== 200) {
      await response.body?.cancel().catch(() => {});
      throw new Error(failure);
    }
    const authorized = JSON.parse(
      (await boundedResponseBody(response, 262144)).toString("utf8"),
    );
    if (
      authorized.ref !== accountRef ||
      authorized.enabled !== true ||
      authorized.state !== "AUTHORIZED" ||
      authorized.authorization?.method !== "API_KEY" ||
      authorized.authorization?.state !== "AUTHORIZED" ||
      typeof authorized.externalAccountMasked !== "string" ||
      !authorized.externalAccountMasked
    )
      throw new Error(failure);
  } catch {
    // Не сохранять cause: сетевое исключение может содержать body/credential.
    throw new Error(failure);
  } finally {
    // Cleanup браузера использует обновлённую сессию даже при потерянном ACK
    // бизнес-запроса. Credential не входит ни в callback, ни в error object.
    if (client) {
      try {
        const cookies = client.authenticatedCookies();
        if (JSON.stringify(cookies).includes(apiKey)) throw new Error(failure);
        await onSessionCookies(cookies);
      } catch {
        throw new Error(failure);
      }
    }
  }
}
