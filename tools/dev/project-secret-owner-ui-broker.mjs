import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { createServer } from "node:https";
import { createSecureContext } from "node:tls";
import { pathToFileURL } from "node:url";
import { readPrivateAgentSecrets } from "./private-agent-secret-reader.mjs";
import { assertPeerUID, readIngressTLS } from "./protected-owner-ui-input.mjs";
import {
  assertInputEnvironment,
  assertOwnerPins,
  readInputProfile,
  readOwnerPins,
  secretInputIntent,
  secretInputKey,
  validateInputProfile,
} from "./project-secret-owner-ui-input.mjs";

export const brokerOrigin = "https://kodex.127.0.0.2.nip.io";
export const brokerHost = "kodex.127.0.0.2.nip.io";
export const brokerPath = "/local-project-secret-input";
export const brokerLifetime = 60_000;
const maximumBody = 256;
function requireInput(valid) {
  if (!valid) throw new Error("PROJECT_SECRET_INPUT_REJECTED");
}
export function profileDigest(profile) {
  validateInputProfile(profile);
  return createHash("sha256").update(JSON.stringify(profile)).digest("hex");
}
export function parseBrokerCLI(args) {
  requireInput(
    args.length === 4 &&
      args[0] === "--profile" &&
      args[2] === "--confirm" &&
      args[3] === secretInputIntent,
  );
  return args[1];
}
function header(request, name) {
  const values = [];
  for (let i = 0; i < request.rawHeaders.length; i += 2)
    if (request.rawHeaders[i].toLowerCase() === name)
      values.push(request.rawHeaders[i + 1]);
  requireInput(values.length === 1);
  return values[0];
}
export function assertBrokerRequest(request, port) {
  requireInput(Number.isInteger(port) && port > 0 && port <= 65535);
  requireInput(
    request.socket.servername === brokerHost &&
      request.socket.getProtocol() === "TLSv1.3" &&
      request.socket.localAddress === "127.0.0.2" &&
      ["127.0.0.1", "127.0.0.2"].includes(request.socket.remoteAddress),
  );
  requireInput(
    header(request, "host") === `${brokerHost}:${port}` &&
      header(request, "origin") === brokerOrigin &&
      request.url === brokerPath &&
      !request.headers.cookie &&
      !request.headers.authorization,
  );
  if (request.method === "OPTIONS") {
    requireInput(
      header(request, "access-control-request-method") === "POST" &&
        header(request, "access-control-request-headers") === "content-type",
    );
    requireInput(
      !request.headers["access-control-request-private-network"] ||
        header(request, "access-control-request-private-network") === "true",
    );
    return "PREFLIGHT";
  }
  requireInput(
    request.method === "POST" &&
      header(request, "content-type") === "application/json" &&
      !request.headers["transfer-encoding"],
  );
  const length = header(request, "content-length");
  requireInput(
    /^[1-9][0-9]{0,2}$/.test(length) && Number(length) <= maximumBody,
  );
  return "INPUT";
}
export function brokerBody(profile) {
  return JSON.stringify({
    intent: secretInputIntent,
    profileDigest: profileDigest(profile),
  });
}
export function createBrokerHandler({
  port,
  profile,
  deadline,
  readPeer,
  readSecrets,
  now = Date.now,
  close,
  output,
}) {
  const expected = Buffer.from(brokerBody(profile));
  let used = false;
  return async (request, response) => {
    let selected,
      consumed = false;
    try {
      requireInput(now() < deadline && !used);
      const mode = assertBrokerRequest(request, port);
      await readPeer(request.socket);
      response.setHeader("Access-Control-Allow-Origin", brokerOrigin);
      response.setHeader("Vary", "Origin");
      response.setHeader("Cache-Control", "no-store");
      response.setHeader("Pragma", "no-cache");
      if (mode === "PREFLIGHT") {
        response.setHeader("Access-Control-Allow-Methods", "POST");
        response.setHeader("Access-Control-Allow-Headers", "Content-Type");
        if (
          request.headers["access-control-request-private-network"] === "true"
        )
          response.setHeader("Access-Control-Allow-Private-Network", "true");
        response.writeHead(204);
        response.end();
        return;
      }
      const chunks = [];
      let size = 0;
      for await (const chunk of request) {
        size += chunk.length;
        requireInput(size <= maximumBody);
        chunks.push(chunk);
      }
      const body = Buffer.concat(chunks);
      try {
        requireInput(body.equals(expected));
      } finally {
        body.fill(0);
      }
      requireInput(
        size === Number(header(request, "content-length")) &&
          now() < deadline &&
          !used,
      );
      used = true;
      consumed = true;
      await readPeer(request.socket);
      requireInput(now() < deadline && !request.socket.destroyed);
      selected = readSecrets([secretInputKey]);
      const token = selected[secretInputKey];
      requireInput(
        typeof token === "string" &&
          token.length > 0 &&
          token.length <= 8192 &&
          !/[\x00-\x1f\x7f]/.test(token),
      );
      requireInput(now() < deadline && !request.socket.destroyed);
      response.setHeader("Content-Type", "application/json");
      response.setHeader("Connection", "close");
      response.once("finish", () => {
        output({ state: "CONSUMED" });
        void close();
      });
      response.end(JSON.stringify({ value: token }));
    } catch {
      if (!response.destroyed) {
        response.setHeader("Cache-Control", "no-store");
        response.writeHead(403);
        response.end();
      }
      if (consumed) void close();
    } finally {
      if (selected) selected[secretInputKey] = "";
    }
  };
}

// Текст содержит только несекретные pins и port; вызывающий получает только Boolean.
export function browserInputScript(port, profile) {
  requireInput(Number.isInteger(port) && port > 0 && port <= 65535);
  validateInputProfile(profile);
  return `async () => {
    const profile = ${JSON.stringify(profile)};
    const requireInput = (valid) => { if (!valid) throw new Error("PROJECT_SECRET_INPUT_REJECTED"); };
    const readOwnerPins = ${readOwnerPins.toString()};
    const assertOwnerPins = ${assertOwnerPins.toString()};
    const read = () => readOwnerPins({evaluate: (fn, pins) => fn(pins)}, profile);
    const fields = () => [...document.querySelectorAll('textarea[name^="runtime-secret-"][name$="-value"]')];
    let value, written = false, field, dialog, name, type;
    const validForm = () => {
      try {
        const items = fields();
        const names = dialog?.querySelectorAll('input[name^="runtime-secret-"][name$="-name"]');
        const types = dialog?.querySelectorAll('select[name^="runtime-secret-"][name$="-value-type"]');
        return location.href === profile.pageURL && location.origin === ${JSON.stringify(brokerOrigin)} &&
          items.length === 1 && items[0] === field && field instanceof HTMLTextAreaElement &&
          document.contains(field) && field.closest('[role="dialog"]') === dialog && dialog &&
          names?.length === 1 && types?.length === 1 && names[0] === name && types[0] === type &&
          name.value === "GH_TOKEN" && !name.disabled && type.value === "STRING" && !type.disabled &&
          !field.disabled && !field.readOnly && field.classList.contains("secret-form__masked") &&
          getComputedStyle(field).webkitTextSecurity === "disc" && field.getClientRects().length > 0;
      } catch { return false; }
    };
    try {
      if (location.href !== profile.pageURL || fields().length !== 1) return false;
      field = fields()[0]; dialog = field.closest('[role="dialog"]');
      name = dialog?.querySelectorAll('input[name^="runtime-secret-"][name$="-name"]')[0];
      type = dialog?.querySelectorAll('select[name^="runtime-secret-"][name$="-value-type"]')[0];
      if (!validForm() || field.value !== "") return false;
      const before = await read(); assertOwnerPins(before, profile);
      if (!validForm() || field.value !== "") return false;
      const after = await read(); assertOwnerPins(after, profile);
      if (JSON.stringify(before) !== JSON.stringify(after) || !validForm() || field.value !== "") return false;
      const response = await fetch(${JSON.stringify(`https://${brokerHost}:${port}${brokerPath}`)}, {
        method:"POST", credentials:"omit", cache:"no-store", redirect:"error",
        signal:AbortSignal.timeout(5000), headers:{"Content-Type":"application/json"},
        body:${JSON.stringify(brokerBody(profile))}
      });
      if (response.status !== 200 || response.url !== ${JSON.stringify(`https://${brokerHost}:${port}${brokerPath}`)}) return false;
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.length > 49152) return false;
      value = JSON.parse(new TextDecoder("utf-8", {fatal:true}).decode(bytes)); bytes.fill(0);
      if (!value || Object.keys(value).length !== 1 || typeof value.value !== "string" ||
          value.value.length === 0 || value.value.length > 8192 || /[\\x00-\\x1f\\x7f]/.test(value.value)) return false;
      const final = await read(); assertOwnerPins(final, profile);
      if (JSON.stringify(final) !== JSON.stringify(after) || !validForm() || field.value !== "") return false;
      written = true; field.value = value.value;
      const ready = () => validForm() && field.value === value.value;
      if (!ready()) return false;
      for (const kind of ["input", "change"]) {
        field.dispatchEvent(new Event(kind, {bubbles:true}));
        if (!ready()) return false;
      }
      written = false; return true;
    } catch { return false; }
    finally {
      if (written && field && document.contains(field)) { try { field.value = ""; field.dispatchEvent(new Event("input", {bubbles:true})); } catch {} }
      if (value) value.value = "";
    }
  }`;
}

export async function main(args, dependencies = {}) {
  let material, server;
  try {
    const path = parseBrokerCLI(args);
    assertInputEnvironment(
      dependencies.environment ?? process.env,
      dependencies.argumentsList ?? process.execArgv,
    );
    requireInput(
      process.platform === "linux" && typeof process.getuid === "function",
    );
    const profile = (dependencies.readProfile ?? readInputProfile)(path);
    material = await (dependencies.readTLS ?? readIngressTLS)();
    const secureContext = createSecureContext({
      ...material,
      minVersion: "TLSv1.3",
      maxVersion: "TLSv1.3",
    });
    server = createServer({
      ...material,
      minVersion: "TLSv1.3",
      maxVersion: "TLSv1.3",
      handshakeTimeout: 5000,
      maxHeaderSize: 4096,
      SNICallback: (name, callback) =>
        callback(
          name === brokerHost
            ? null
            : new Error("PROJECT_SECRET_INPUT_REJECTED"),
          name === brokerHost ? secureContext : undefined,
        ),
    });
    const sockets = new Set();
    server.on("connection", (socket) => {
      sockets.add(socket);
      socket.once("close", () => sockets.delete(socket));
    });
    let closing, timer;
    const close = () => {
      if (closing) return closing;
      clearTimeout(timer);
      closing = new Promise((resolve) => {
        const force = setTimeout(() => {
          server.closeAllConnections();
          for (const socket of sockets) socket.destroy();
        }, 1000);
        server.close(() => {
          clearTimeout(force);
          resolve();
        });
        server.closeIdleConnections();
      });
      return closing;
    };
    const output =
      dependencies.output ??
      ((value) => process.stdout.write(JSON.stringify(value) + "\n"));
    server.requestTimeout = 5000;
    server.headersTimeout = 5000;
    server.keepAliveTimeout = 1000;
    server.on("tlsClientError", () => {});
    server.on("upgrade", (_request, socket) => socket.destroy());
    await new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.2", resolve);
    });
    server.removeAllListeners("error");
    server.on("error", () => {
      output({ state: "FAILED" });
      void close();
    });
    const port = server.address().port;
    const handler = createBrokerHandler({
      port,
      profile,
      deadline: Date.now() + brokerLifetime,
      readSecrets: readPrivateAgentSecrets,
      readPeer: async (socket) =>
        assertPeerUID(
          await readFile("/proc/net/tcp", "utf8"),
          socket,
          process.getuid(),
        ),
      close,
      output,
    });
    server.on("request", (request, response) => {
      void handler(request, response).catch(() => {
        request.socket.destroy();
        void close();
      });
    });
    timer = setTimeout(() => {
      output({ state: "EXPIRED" });
      void close();
    }, brokerLifetime);
    const onSignal = () => {
      void close();
    };
    process.once("SIGINT", onSignal);
    process.once("SIGTERM", onSignal);
    output({ state: "READY", port, profileDigest: profileDigest(profile) });
    await new Promise((resolve) => server.once("close", resolve));
    process.removeListener("SIGINT", onSignal);
    process.removeListener("SIGTERM", onSignal);
    return 0;
  } catch {
    server?.closeAllConnections();
    server?.close();
    (
      dependencies.output ??
      ((value) => process.stdout.write(JSON.stringify(value) + "\n"))
    )({ state: "FAILED" });
    return 1;
  } finally {
    material?.key.fill(0);
    material?.cert.fill(0);
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  process.exitCode = await main(process.argv.slice(2));
