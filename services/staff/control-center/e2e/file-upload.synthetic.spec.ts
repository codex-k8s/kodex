import { createHash } from "node:crypto";
import { createServer } from "node:http";
import { expect, test } from "@playwright/test";
import { installSyntheticAbortObserver } from "./synthetic-abort-observer";

// Отдельный loopback wire proof: без Kodex API, credentials и внешних effects.
test("synthetic: File upload доставляет точные bytes независимо от protocol readback", async ({
  page,
}, testInfo) => {
  const content = "---\nname: Synthetic\n---\nSynthetic instructions\n";
  const expected = Buffer.from(content, "utf8");
  const digest = createHash("sha256").update(expected).digest("hex");
  const receipts: Array<{
    bytes: number;
    digest: string;
    mediaType: string | undefined;
  }> = [];
  const server = createServer((request, response) => {
    if (request.method === "GET" && request.url === "/") {
      response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      response.end("<!doctype html><title>Synthetic upload</title>");
      return;
    }
    if (request.method === "GET" && request.url === "/api/v1/slow-body") {
      response.writeHead(200, { "content-type": "text/plain" });
      response.flushHeaders();
      response.write("partial");
      return;
    }
    if (request.method !== "POST" || request.url !== "/api/v1/upload") {
      response.writeHead(404).end();
      return;
    }
    const chunks: Buffer[] = [];
    let bytes = 0;
    request.on("data", (chunk: Buffer) => {
      bytes += chunk.length;
      if (bytes > 4096) {
        response.writeHead(413).end();
        request.destroy();
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () => {
      const receipt = {
        bytes,
        digest: createHash("sha256")
          .update(Buffer.concat(chunks))
          .digest("hex"),
        mediaType: request.headers["content-type"],
      };
      receipts.push(receipt);
      response.writeHead(201, { "content-type": "application/json" });
      response.end(JSON.stringify(receipt));
    });
  });
  server.requestTimeout = 5000;
  server.headersTimeout = 5000;
  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    const address = server.address();
    if (!address || typeof address === "string")
      throw new Error("Missing loopback listener address");
    const origin = `http://127.0.0.1:${String(address.port)}`;
    const aborted: string[] = [];
    await installSyntheticAbortObserver(
      page,
      (event) => {
        if (event.phase === "abort") aborted.push(new URL(event.url).pathname);
      },
      origin,
    );
    await page.goto(`${origin}/`);
    const actual = await page.evaluate(async (body) => {
      const file = new File([body], "SKILL.md", { type: "text/markdown" });
      const response = await fetch("/api/v1/upload", {
        method: "POST",
        body: file,
        credentials: "omit",
      });
      return {
        status: response.status,
        receipt: (await response.json()) as unknown,
      };
    }, content);
    const expectedReceipt = {
      bytes: expected.length,
      digest,
      mediaType: "text/markdown",
    };
    expect(actual).toEqual({ status: 201, receipt: expectedReceipt });
    expect(receipts).toEqual([expectedReceipt]);
    const cancellation = await page.evaluate(async () => {
      const controller = new AbortController();
      const pending = fetch("/api/v1/cancelled", {
        signal: controller.signal,
      }).catch((error: unknown) =>
        error instanceof DOMException ? error.name : "UNKNOWN",
      );
      controller.abort();
      return pending;
    });
    expect(cancellation).toBe("AbortError");
    await expect.poll(() => aborted).toEqual(["/api/v1/cancelled"]);
    const bodyCancellation = await page.evaluate(async () => {
      const controller = new AbortController();
      const response = await fetch("/api/v1/slow-body", {
        signal: controller.signal,
      });
      controller.abort();
      return response
        .text()
        .catch((error: unknown) =>
          error instanceof DOMException ? error.name : "UNKNOWN",
        );
    });
    expect(bodyCancellation).toBe("AbortError");
    await expect
      .poll(() => aborted)
      .toEqual(["/api/v1/cancelled", "/api/v1/slow-body"]);
    expect(receipts).toEqual([expectedReceipt]);
    testInfo.annotations.push({
      type: "upload-wire",
      description: `loopback File bytes=${String(expected.length)} sha256=${digest}; not Kodex/vendor upload`,
    });
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
});
