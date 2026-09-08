import { test, expect } from "@playwright/test";
import { createServer } from "node:http";
import { createHash } from "node:crypto";
import type { Duplex } from "node:stream";
import {
  installProtocolObserver,
  observeFrame,
  resumedAfterRenewal,
  socketProof,
  type SocketProof,
} from "./session-renewal-proof";

// Локальный protocol fixture проверяет наблюдатель. Это не OIDC, PWA refresh
// или ticket security proof и никогда не обращается к staging.
test("synthetic: native WS observer различает две вкладки и замену sockets", async ({
  context,
}) => {
  const connections = new Set<Duplex>();
  const server = createServer((_request, response) => {
    response.setHeader("Content-Type", "text/html");
    response.end("<!doctype html><title>Session observer fixture</title>");
  });
  server.on("upgrade", (request, socket) => {
    const key = request.headers["sec-websocket-key"];
    if (typeof key !== "string" || request.url !== "/session/stream") {
      socket.destroy();
      return;
    }
    connections.add(socket);
    socket.on("close", () => connections.delete(socket));
    const accept = createHash("sha1")
      .update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
      .digest("base64");
    socket.write(
      `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\nSec-WebSocket-Protocol: kodex.session.v2\r\n\r\n`,
    );
    let sent = false;
    socket.on("data", (data: Buffer) => {
      if ((Number(data[0]) & 0x0f) === 8) {
        socket.end(Buffer.from([0x88, 0]));
        return;
      }
      if (sent) return;
      sent = true;
      const payload = Buffer.from(
        JSON.stringify({
          type: "SESSION_READY",
          streams: [
            { streamKind: "PLATFORM", streamRef: "PLATFORM", cursor: 7 },
          ],
        }),
      );
      const header = Buffer.alloc(payload.length < 126 ? 2 : 4);
      header[0] = 0x81;
      header[1] = payload.length < 126 ? payload.length : 126;
      if (payload.length >= 126) header.writeUInt16BE(payload.length, 2);
      socket.write(Buffer.concat([header, payload]));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Local fixture unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  try {
    await context.addInitScript(installProtocolObserver);
    const tabs: SocketProof[][] = [[], []];
    const pages = await Promise.all([context.newPage(), context.newPage()]);
    for (const [index, page] of pages.entries()) {
      page.on("websocket", (socket) => {
        const proof = socketProof();
        tabs[index]?.push(proof);
        socket.on("framesent", ({ payload }) =>
          observeFrame(proof, payload, "sent"),
        );
        socket.on("framereceived", ({ payload }) =>
          observeFrame(proof, payload, "received"),
        );
        socket.on("close", () => {
          proof.closed = true;
        });
      });
      await page.goto(origin);
    }
    for (let generation = 0; generation < 2; generation++) {
      await Promise.all(
        pages.map((page) =>
          page.evaluate(
            (wsOrigin) =>
              new Promise<void>((resolve) => {
                const holder = window as unknown as {
                  fixtureSocket?: WebSocket;
                };
                holder.fixtureSocket?.close();
                const socket = new WebSocket(`${wsOrigin}/session/stream`, [
                  "kodex.session.v2",
                  "ticket.synthetic-fixture",
                ]);
                holder.fixtureSocket = socket;
                socket.addEventListener("open", () =>
                  socket.send(
                    JSON.stringify({
                      type: "SESSION_RESUME",
                      platformAfterSequence: 7,
                    }),
                  ),
                );
                socket.addEventListener("message", () => resolve(), {
                  once: true,
                });
              }),
            origin.replace("http:", "ws:"),
          ),
        ),
      );
    }
    await expect.poll(() => tabs.every(resumedAfterRenewal)).toBe(true);
    for (const page of pages) {
      expect(
        await page.evaluate(
          () =>
            (window as unknown as { __kodexSessionProofProtocols: string[] })
              .__kodexSessionProofProtocols,
        ),
      ).toEqual(["v2", "v2"]);
    }
  } finally {
    await Promise.all(context.pages().map((page) => page.close()));
    for (const socket of connections) socket.destroy();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
