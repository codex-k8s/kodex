import { createHash } from "node:crypto";
import { createServer } from "node:http";
import type { Duplex } from "node:stream";
import { expect, test } from "@playwright/test";

test("realtime: retired CONNECTING socket закрывается после open и по bounded timeout", async ({
  page,
  browserName,
}) => {
  const sockets = new Set<Duplex>();
  let applicationFrames = 0;
  const server = createServer();
  server.on("upgrade", (request, socket) => {
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
    if (request.url === "/never") return;
    const key = request.headers["sec-websocket-key"];
    if (request.url !== "/open" || typeof key !== "string") {
      socket.destroy();
      return;
    }
    setTimeout(() => {
      if (socket.destroyed) return;
      const accept = createHash("sha1")
        .update(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
        .digest("base64");
      socket.write(
        `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`,
      );
    }, 75);
    socket.on("data", (data: Buffer) => {
      if ((Number(data[0]) & 0x0f) !== 8) {
        applicationFrames++;
        return;
      }
      socket.end(Buffer.from([0x88, 0]));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Local fixture unavailable");
  const consoleErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.type());
  });
  const fixture = async (path: string) => {
    const socket = `ws://127.0.0.1:${String(address.port)}${path}`;
    await page.goto(
      `http://127.0.0.1:43122/e2e/fixtures/realtime-retirement.html?socket=${encodeURIComponent(socket)}`,
    );
    await expect(page.locator("#state")).toHaveText("closed", {
      timeout: 8_000,
    });
    expect(await page.locator("html").getAttribute("data-message")).toBeNull();
  };
  try {
    await fixture("/open");
    expect(applicationFrames).toBe(0);
    expect(consoleErrors).toEqual([]);
    await fixture("/never");
    expect(await page.locator("html").getAttribute("data-opened")).toBeNull();
    // По тайм-ауту намеренно вызывается стандартный отказ соединения.
    // Firefox/WebKit выпускают console.error, Chromium закрывает его без записи;
    // обе ветки измеряются напрямую, а global console filter не добавляется.
    if (browserName === "chromium") expect(consoleErrors).toEqual([]);
    else expect(consoleErrors.length).toBeGreaterThan(0);
  } finally {
    for (const socket of sockets) socket.destroy();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
