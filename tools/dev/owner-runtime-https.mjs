import { Agent, request } from "node:https";
import { Readable } from "node:stream";
import { rootCertificates } from "node:tls";
import { constants, openSync, fstatSync, readSync, closeSync } from "node:fs";

const transportFailure = "Owner runtime HTTPS transport failed";
// Полное поколение cookies допускает до10 строк по4608 bytes; остаётся ограниченный запас для остальных headers.
// Валидация имён, количества, значений и attributes остаётся в существующем session consumer.
const maximumResponseHeaderBytes = 64 * 1024;

// CA читается один раз из exact regular descriptor; symlink, hardlink и unbounded content отклоняются.
export function readOwnerRuntimeCA(path) {
  if (!path) return undefined;
  const fd = openSync(
    path,
    constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
  );
  let body;
  try {
    const info = fstatSync(fd);
    if (
      !info.isFile() ||
      info.nlink !== 1 ||
      info.size < 1 ||
      info.size > 1048576
    )
      throw Error(transportFailure);
    body = Buffer.alloc(info.size + 1);
    const count = readSync(fd, body, 0, body.length, null);
    if (count !== info.size) throw Error(transportFailure);
    return body.subarray(0, count);
  } catch {
    body?.fill(0);
    throw Error(transportFailure);
  } finally {
    closeSync(fd);
  }
}

// Node fetch adapter сохраняет штатный session consumer, но закрывает TLS>=1.3/exact SNI и bounded headers/body/lifetime.
// Trust по умолчанию — bundled Node CA; caller может передать только явно прочитанную owner-local CA, без skipTLSVerify.
export function createOwnerRuntimeHTTPS({ ca = rootCertificates } = {}) {
  const agent = new Agent({
    keepAlive: true,
    maxSockets: 2,
    maxFreeSockets: 1,
    maxTotalSockets: 2,
    minVersion: "TLSv1.3",
    rejectUnauthorized: true,
    ca,
  });
  const sockets = new Set(),
    requests = new Set();
  let closed = false;
  async function fetchAPI(url, options = {}) {
    url = new URL(url);
    if (
      closed ||
      url.protocol !== "https:" ||
      url.username ||
      url.password ||
      url.hash ||
      !["GET", "POST", "PUT"].includes(options.method ?? "GET") ||
      !["error", "manual"].includes(options.redirect) ||
      !(options.signal instanceof AbortSignal) ||
      options.signal.aborted ||
      (options.body !== undefined && typeof options.body !== "string") ||
      Buffer.byteLength(options.body ?? "") > 65536
    )
      throw Error(transportFailure);
    return new Promise((resolve, reject) => {
      const signal = options.signal;
      const req = request(
        url,
        {
          method: options.method ?? "GET",
          headers: Object.fromEntries(new Headers(options.headers)),
          agent,
          minVersion: "TLSv1.3",
          rejectUnauthorized: true,
          servername: url.hostname,
          ca,
          maxHeaderSize: maximumResponseHeaderBytes,
        },
        (response) => {
          const headers = new Headers();
          for (let index = 0; index < response.rawHeaders.length; index += 2)
            headers.append(
              response.rawHeaders[index],
              response.rawHeaders[index + 1],
            );
          const status = response.statusCode;
          if (!Number.isInteger(status) || status < 200 || status > 599) {
            response.destroy();
            reject(Error(transportFailure));
            return;
          }
          // Redirect возвращается caller для closed отказа; adapter никогда не следует Location.
          try {
            resolve(
              new Response(
                [204, 205, 304].includes(status)
                  ? null
                  : Readable.toWeb(response),
                { status, headers },
              ),
            );
            if ([204, 205, 304].includes(status)) response.resume();
          } catch {
            response.destroy();
            reject(Error(transportFailure));
          }
        },
      );
      const abort = () => req.destroy(Error(transportFailure));
      requests.add(req);
      req.once("socket", (socket) => {
        if (!sockets.has(socket)) {
          sockets.add(socket);
          socket.once("close", () => sockets.delete(socket));
        }
      });
      req.once("close", () => {
        requests.delete(req);
        signal.removeEventListener("abort", abort);
      });
      req.once("error", () => reject(Error(transportFailure)));
      req.setTimeout(30000, abort);
      signal.addEventListener("abort", abort, { once: true });
      if (signal.aborted) abort();
      req.end(options.body);
    });
  }
  async function close(signal) {
    closed = true;
    for (const req of requests) req.destroy(Error(transportFailure));
    const joined = Promise.all(
      [...sockets].map(
        (socket) =>
          new Promise((resolve) => {
            socket.once("close", resolve);
            socket.destroy();
          }),
      ),
    );
    agent.destroy();
    if (!signal || signal.aborted) throw Error(transportFailure);
    let aborted;
    try {
      await Promise.race([
        joined,
        new Promise((_, reject) => {
          aborted = () => reject(Error(transportFailure));
          signal.addEventListener("abort", aborted, { once: true });
        }),
      ]);
    } finally {
      signal.removeEventListener("abort", aborted);
    }
    if (sockets.size || requests.size) throw Error(transportFailure);
  }
  return { fetchAPI, close };
}
