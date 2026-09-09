import { createServer } from "node:http";
import { expect, test } from "@playwright/test";
import { SessionBoundaryDiagnostics } from "./session-boundary-diagnostics";

// Проверяется только browser observer; это не live OIDC/renewal acceptance.
test("session boundary observer сохраняет200/401 и refresh без secret payload", async ({
  page,
}) => {
  let renewed = false;
  const server = createServer((request, response) => {
    if (request.url === "/api/v1/session/ticket") {
      response.setHeader("Content-Type", "application/json");
      response.end(JSON.stringify({ ticket: "private-ticket" }));
      return;
    }
    if (request.url === "/api/v1/session") {
      response.setHeader("Content-Type", "application/json");
      if (request.method === "PUT") renewed = true;
      if (request.headers["x-fixture-expired"] === "1") {
        response.statusCode = 401;
        response.end(JSON.stringify({ detail: "private-error" }));
        return;
      }
      response.end(
        JSON.stringify({
          version: renewed ? 4 : 3,
          generation: "private-generation",
          actor: "private-actor",
          renewalMode: "BACKEND_REFRESH",
          serverTime: renewed ? "2026-09-09T10:04:30Z" : "2026-09-09T10:00:00Z",
          accessExpiresAt: "2026-09-09T10:09:30Z",
          expiresAt: "2026-09-09T10:19:30Z",
          absoluteExpiresAt: "2026-09-09T11:00:00Z",
          renewAfter: "2026-09-09T10:09:00Z",
        }),
      );
      return;
    }
    response.setHeader("Content-Type", "text/html");
    response.end("<!doctype html><title>Session diagnostic fixture</title>");
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture listener unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  const diagnostics = new SessionBoundaryDiagnostics();
  const finish = diagnostics.install(page, origin);
  try {
    await page.goto(origin);
    await page.evaluate(async () => {
      await (await fetch("/api/v1/session")).text();
      await (await fetch("/api/v1/session", { method: "PUT" })).text();
      await (
        await fetch("/api/v1/session", {
          headers: { "x-fixture-expired": "1" },
        })
      ).text();
      await (await fetch("/api/v1/session/ticket", { method: "POST" })).text();
    });
    await expect.poll(() => diagnostics.snapshot().events.length).toBe(4);
    const observed = diagnostics.snapshot();
    expect(observed.overflow).toBe(0);
    expect(observed.events).toMatchObject([
      {
        category: "SESSION_READ",
        status: 200,
        boundary: { version: 3, absoluteRemainingMs: 3600000 },
      },
      {
        category: "SESSION_RENEW",
        status: 200,
        boundary: { version: 4, absoluteRemainingMs: 3330000 },
      },
      { category: "SESSION_READ", status: 401, metadata: "NOT_APPLICABLE" },
      { category: "SESSION_TICKET", status: 200, metadata: "NOT_APPLICABLE" },
    ]);
    expect(JSON.stringify(observed)).not.toContain("private");
  } finally {
    await page.close();
    await finish();
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});
