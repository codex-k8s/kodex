import type { BrowserContext, WebSocketRoute } from "@playwright/test";
import type { OwnerSessionMetadata } from "../../src/shared/api/generated/openapi/types.gen";

export function syntheticBrowserSession(
  overrides: Partial<OwnerSessionMetadata> = {},
): OwnerSessionMetadata {
  const now = Date.now();
  return {
    generation: "11111111-1111-4111-8111-111111111111",
    version: 1,
    sessionRevision: 1,
    serverTime: new Date(now).toISOString(),
    expiresAt: new Date(now + 20 * 60_000).toISOString(),
    accessExpiresAt: new Date(now + 10 * 60_000).toISOString(),
    absoluteExpiresAt: new Date(now + 60 * 60_000).toISOString(),
    renewAfter: new Date(now + 5 * 60_000).toISOString(),
    renewalMode: "BACKEND_REFRESH",
    ...overrides,
  };
}

// Устанавливает только синтетический CSRF marker в заданный browser context; вызывающий
// выбирает значение и отвечает за дальнейшие маршруты и lifecycle.
export function installSyntheticCsrf(
  context: Pick<BrowserContext, "addCookies">,
  character: string,
): Promise<void> {
  return context.addCookies([
    {
      name: "__Host-kodex-csrf",
      value: character.repeat(43),
      domain: "kodex.test",
      path: "/",
      secure: true,
      sameSite: "Strict",
    },
  ]);
}

// Регистрирует synthetic socket и завершает close handshake с прежними значениями по умолчанию.
export function trackSyntheticSocket<
  T extends { socket: WebSocketRoute; closed: boolean },
>(connections: T[], connection: T): void {
  connections.push(connection);
  connection.socket.onClose((code, reason) => {
    connection.closed = true;
    void connection.socket.close({ code: code ?? 1000, reason: reason ?? "" });
  });
}
