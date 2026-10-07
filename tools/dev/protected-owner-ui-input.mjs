import { execFile } from "node:child_process";
import {
  createPrivateKey,
  createPublicKey,
  X509Certificate,
} from "node:crypto";
import { lstat, mkdtemp, readFile, realpath, rm } from "node:fs/promises";
import { createServer } from "node:https";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { createSecureContext } from "node:tls";
import { promisify } from "node:util";
import { pathToFileURL } from "node:url";
import { readPrivateAgentSecrets } from "./private-agent-secret-reader.mjs";

export const inputOrigin = "https://sso.kodex.127.0.0.2.nip.io";
export const inputHost = "kodex.127.0.0.2.nip.io";
export const inputPath = "/local-owner-sso";
export const inputIntent = "KODEX_LOCAL_OWNER_SSO";
const ownerKeys = ["KODEX_LOCAL_OWNER_USERNAME", "KODEX_LOCAL_OWNER_PASSWORD"];
const lifetime = 60_000;
const maximumBody = 256;
const namespace = "kodex-system";
const ingressName = "staff-control-center";
const secretName = "staff-control-center-public-tls";
const execute = promisify(execFile);

function requireInput(valid) {
  if (!valid) throw new Error("OWNER_UI_INPUT_REJECTED");
}

export function parseInputCLI(args) {
  requireInput(
    args.length === 2 && args[0] === "--confirm" && args[1] === inputIntent,
  );
}

export function assertInputEnvironment(environment, argumentsList = []) {
  requireInput(
    ![
      "DEBUG",
      "PWDEBUG",
      "NODE_OPTIONS",
      "NODE_DEBUG",
      "NODE_DEBUG_NATIVE",
      "SSLKEYLOGFILE",
    ].some((name) => environment[name]),
  );
  requireInput(argumentsList.length === 0);
}

function header(request, name) {
  const values = [];
  for (let index = 0; index < request.rawHeaders.length; index += 2)
    if (request.rawHeaders[index].toLowerCase() === name)
      values.push(request.rawHeaders[index + 1]);
  requireInput(values.length === 1);
  return values[0];
}

export function assertInputRequest(request, port) {
  requireInput(Number.isInteger(port) && port > 0 && port <= 65535);
  requireInput(request.socket.servername === inputHost);
  requireInput(request.socket.getProtocol() === "TLSv1.3");
  requireInput(request.socket.localAddress === "127.0.0.2");
  requireInput(
    ["127.0.0.1", "127.0.0.2"].includes(request.socket.remoteAddress),
  );
  requireInput(header(request, "host") === `${inputHost}:${port}`);
  requireInput(header(request, "origin") === inputOrigin);
  requireInput(request.url === inputPath);
  requireInput(!request.headers.cookie && !request.headers.authorization);
  if (request.method === "OPTIONS") {
    requireInput(header(request, "access-control-request-method") === "POST");
    requireInput(
      header(request, "access-control-request-headers") === "content-type",
    );
    requireInput(
      !request.headers["access-control-request-private-network"] ||
        header(request, "access-control-request-private-network") === "true",
    );
    return "PREFLIGHT";
  }
  requireInput(request.method === "POST");
  requireInput(header(request, "content-type") === "application/json");
  requireInput(!request.headers["transfer-encoding"]);
  const length = header(request, "content-length");
  requireInput(
    /^[1-9][0-9]{0,2}$/.test(length) && Number(length) <= maximumBody,
  );
  return "INPUT";
}

export function parseInputBody(buffer) {
  requireInput(
    Buffer.isBuffer(buffer) &&
      buffer.length > 0 &&
      buffer.length <= maximumBody,
  );
  // Закрытое каноническое тело не допускает дубликатов или дополнительных полей.
  requireInput(
    buffer.equals(Buffer.from(JSON.stringify({ intent: inputIntent }))),
  );
}

function tcpAddress(address, port) {
  requireInput(["127.0.0.1", "127.0.0.2"].includes(address));
  requireInput(Number.isInteger(port) && port > 0 && port <= 65535);
  const ip = address
    .split(".")
    .reverse()
    .map((part) => Number(part).toString(16).padStart(2, "0"))
    .join("");
  return `${ip}:${port.toString(16).padStart(4, "0")}`.toUpperCase();
}

export function assertPeerUID(source, socket, uid) {
  requireInput(
    Number.isSafeInteger(uid) &&
      uid >= 0 &&
      typeof source === "string" &&
      source.length <= 1 << 20,
  );
  const local = tcpAddress(socket.remoteAddress, socket.remotePort);
  const remote = tcpAddress(socket.localAddress, socket.localPort);
  // Нужна клиентская запись reverse tuple, не UID серверного listening socket.
  const matches = source
    .trim()
    .split("\n")
    .slice(1)
    .map((line) => line.trim().split(/\s+/))
    .filter(
      (fields) =>
        fields[1] === local && fields[2] === remote && fields[3] === "01",
    );
  requireInput(
    matches.length === 1 &&
      /^[0-9]+$/.test(matches[0][7]) &&
      Number(matches[0][7]) === uid,
  );
}

export function parseIngress(source) {
  const value = JSON.parse(source);
  requireInput(
    value.kind === "Ingress" &&
      value.metadata?.name === ingressName &&
      value.metadata?.namespace === namespace,
  );
  requireInput(value.spec?.rules?.some((rule) => rule.host === inputHost));
  requireInput(
    value.spec?.tls?.some(
      (item) =>
        item.secretName === secretName && item.hosts?.includes(inputHost),
    ),
  );
}

export function parseTLSSecret(source) {
  const value = JSON.parse(source);
  requireInput(
    value.kind === "Secret" &&
      value.type === "kubernetes.io/tls" &&
      value.metadata?.name === secretName &&
      value.metadata?.namespace === namespace,
  );
  const decode = (name) => {
    const text = value.data?.[name];
    requireInput(
      typeof text === "string" &&
        text.length > 0 &&
        text.length <= 131072 &&
        /^[A-Za-z0-9+/]+={0,2}$/.test(text),
    );
    const buffer = Buffer.from(text, "base64");
    requireInput(buffer.toString("base64") === text);
    return buffer;
  };
  return { cert: decode("tls.crt"), key: decode("tls.key") };
}

export async function withPrivateKubectlCache(action, io = {}) {
  const root = await (io.realpath ?? realpath)((io.tmpdir ?? tmpdir)());
  requireInput(["/tmp", "/var/tmp"].includes(root));
  const directory = await (io.mkdtemp ?? mkdtemp)(
    join(root, "kodex-owner-ui-cache-"),
  );
  const metadata = io.lstat ?? lstat;
  const uid = io.uid ?? process.getuid();
  let identity;
  const verify = (info) =>
    requireInput(
      info.isDirectory() &&
        !info.isSymbolicLink() &&
        info.uid === uid &&
        (info.mode & 0o777) === 0o700,
    );
  try {
    requireInput(
      dirname(directory) === root &&
        /^kodex-owner-ui-cache-[A-Za-z0-9]{6}$/.test(basename(directory)),
    );
    identity = await metadata(directory);
    verify(identity);
    return await action(directory);
  } finally {
    if (identity) {
      const current = await metadata(directory);
      verify(current);
      requireInput(
        current.dev === identity.dev && current.ino === identity.ino,
      );
      await (io.rm ?? rm)(directory, {
        recursive: true,
        force: false,
        maxRetries: 0,
      });
    }
  }
}

export function ownerTLSReadArguments(kind, name, directory) {
  requireInput(
    (kind === "ingress" && name === ingressName) ||
      (kind === "secret" && name === secretName),
  );
  requireInput(
    directory.startsWith("/") &&
      ["/tmp", "/var/tmp"].includes(dirname(directory)) &&
      /^kodex-owner-ui-cache-[A-Za-z0-9]{6}$/.test(basename(directory)),
  );
  return [
    "--kubeconfig",
    "/home/s/.kube/config",
    "--context",
    "k3d-kodex",
    "--namespace",
    namespace,
    "--cache-dir",
    directory,
    "get",
    kind,
    name,
    "--output",
    "json",
  ];
}

export async function readIngressTLS() {
  const material = await withPrivateKubectlCache(async (cacheDirectory) => {
    const kubectl = async (kind, name) => {
      try {
        const result = await execute(
          "/home/s/.local/state/kodex-dev/tools/bin/kubectl",
          ownerTLSReadArguments(kind, name, cacheDirectory),
          {
            env: {
              PATH: "/usr/local/bin:/usr/bin:/bin",
              KUBECONFIG: "/home/s/.kube/config",
            },
            encoding: "utf8",
            timeout: 10_000,
            maxBuffer: 262144,
          },
        );
        return result.stdout;
      } catch {
        throw new Error("OWNER_UI_INPUT_REJECTED");
      }
    };
    parseIngress(await kubectl("ingress", ingressName));
    return parseTLSSecret(await kubectl("secret", secretName));
  });
  try {
    const certificate = new X509Certificate(material.cert);
    requireInput(
      certificate.checkHost(inputHost, { wildcards: false }) === inputHost,
    );
    requireInput(
      Date.parse(certificate.validFrom) <= Date.now() &&
        Date.parse(certificate.validTo) > Date.now() + lifetime,
    );
    const privatePublic = createPublicKey(
      createPrivateKey(material.key),
    ).export({ type: "spki", format: "der" });
    requireInput(
      privatePublic.equals(
        certificate.publicKey.export({ type: "spki", format: "der" }),
      ),
    );
    return material;
  } catch {
    material.key.fill(0);
    material.cert.fill(0);
    throw new Error("OWNER_UI_INPUT_REJECTED");
  }
}

export function createInputHandler({
  port,
  deadline,
  readPeer,
  readSecrets,
  now = Date.now,
  close,
  output,
}) {
  let used = false;
  return async (request, response) => {
    let credentials;
    let consumed = false;
    try {
      requireInput(now() < deadline && !used);
      const mode = assertInputRequest(request, port);
      await readPeer(request.socket);
      response.setHeader("Access-Control-Allow-Origin", inputOrigin);
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
        parseInputBody(body);
      } finally {
        body.fill(0);
      }
      requireInput(
        size === Number(header(request, "content-length")) &&
          now() < deadline &&
          !used,
      );
      // Поглощение предшествует чтению; конкурентный POST не выдаст вторую пару.
      used = true;
      consumed = true;
      await readPeer(request.socket);
      requireInput(now() < deadline && !request.socket.destroyed);
      credentials = readSecrets(ownerKeys);
      requireInput(
        ownerKeys.every(
          (key) =>
            typeof credentials[key] === "string" && credentials[key].length > 0,
        ),
      );
      requireInput(now() < deadline && !request.socket.destroyed);
      response.setHeader("Content-Type", "application/json");
      response.setHeader("Connection", "close");
      response.once("finish", () => {
        output({ state: "CONSUMED" });
        void close();
      });
      response.end(
        JSON.stringify({
          username: credentials.KODEX_LOCAL_OWNER_USERNAME,
          password: credentials.KODEX_LOCAL_OWNER_PASSWORD,
        }),
      );
    } catch {
      if (!response.destroyed) {
        response.setHeader("Cache-Control", "no-store");
        response.writeHead(403);
        response.end();
      }
      if (consumed) void close();
    } finally {
      if (credentials) for (const key of ownerKeys) credentials[key] = "";
    }
  };
}

export function browserInputScript(port) {
  requireInput(Number.isInteger(port) && port > 0 && port <= 65535);
  return `async () => {
    if (location.origin !== ${JSON.stringify(inputOrigin)}) return false;
    const username = document.querySelector("#username"), attempted = document.querySelector("#kc-attempted-username"), password = document.querySelector("#password"), button = document.querySelector("#kc-login");
    const identity = username || attempted;
    const validForm = () => {
      try {
        return location.origin === ${JSON.stringify(inputOrigin)} && identity instanceof HTMLInputElement && password instanceof HTMLInputElement && password.type === "password" && button && document.contains(identity) && document.contains(password) && document.contains(button) && document.querySelector("#username") === username && document.querySelector("#kc-attempted-username") === attempted && document.querySelector("#password") === password && document.querySelector("#kc-login") === button && identity.disabled === false && password.disabled === false && button.disabled === false && password.form && password.form === button.form && password.form.method.toLowerCase() === "post" && new URL(password.form.action).origin === location.origin && (username ? username.form === password.form : attempted.type === "text" && attempted.readOnly === true && attempted.hasAttribute("readonly") && attempted.form === null && password.form.id === "kc-form-login" && document.querySelector("#kc-form-login") === password.form);
      } catch { return false; }
    };
    if (!validForm()) return false;
    const form = password.form, action = form.action, originalIdentity = identity.value, originalPassword = password.value;
    const stableForm = () => validForm() && password.form === form && form.action === action;
    let value, written = false, submitted = false;
    try {
      const response = await fetch(${JSON.stringify(`https://${inputHost}:${port}${inputPath}`)}, {method:"POST", credentials:"omit", cache:"no-store", redirect:"error", headers:{"Content-Type":"application/json"}, body:${JSON.stringify(JSON.stringify({ intent: inputIntent }))}});
      if (!response.ok) return false;
      value = await response.json();
      if (Object.keys(value).length !== 2 || ![value.username, value.password].every((text) => typeof text === "string" && text.length > 0 && text.length <= 16384)) return false;
      if (!stableForm() || identity.value !== originalIdentity || password.value !== originalPassword || (!username && identity.value !== value.username)) return false;
      written = true;
      if (username) username.value = value.username;
      password.value = value.password;
      const ready = () => stableForm() && identity.value === value.username && password.value === value.password;
      if (!ready()) return false;
      for (const input of username ? [username, password] : [password]) for (const type of ["input", "change"]) {
        input.dispatchEvent(new Event(type, {bubbles:true}));
        if (!ready()) return false;
      }
      button.click(); submitted = true; return true;
    } catch { return false; }
    finally {
      if (written && !submitted) for (const input of username ? [username, password] : [password]) { try { input.value = ""; } catch {} }
      if (value) { value.username = ""; value.password = ""; }
    }
  }`;
}

export async function main(args) {
  let material;
  let server;
  try {
    parseInputCLI(args);
    requireInput(
      process.platform === "linux" && typeof process.getuid === "function",
    );
    assertInputEnvironment(process.env, process.execArgv);
    material = await readIngressTLS();
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
          name === inputHost ? null : new Error("OWNER_UI_INPUT_REJECTED"),
          name === inputHost ? secureContext : undefined,
        ),
    });
    let closing;
    let timer;
    const sockets = new Set();
    server.on("connection", (socket) => {
      sockets.add(socket);
      socket.once("close", () => sockets.delete(socket));
    });
    const output = (value) =>
      process.stdout.write(`${JSON.stringify(value)}\n`);
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
    const deadline = Date.now() + lifetime;
    const handler = createInputHandler({
      port,
      deadline,
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
    }, lifetime);
    const onSignal = () => {
      void close();
    };
    process.once("SIGTERM", onSignal);
    process.once("SIGINT", onSignal);
    output({ state: "READY", port });
    await new Promise((resolve) => server.once("close", resolve));
    process.removeListener("SIGTERM", onSignal);
    process.removeListener("SIGINT", onSignal);
    return 0;
  } catch {
    server?.closeAllConnections();
    server?.close();
    process.stdout.write('{"state":"FAILED"}\n');
    return 1;
  } finally {
    material?.key.fill(0);
    material?.cert.fill(0);
  }
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url)
  process.exitCode = await main(process.argv.slice(2));
