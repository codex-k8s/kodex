import { createServer } from "node:net";
import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import {
  decodePortForwardOutput,
  encodePortForwardInput,
  portForwardHeaderBytes,
} from "../../services/staff/control-center/src/shared/lib/kubernetes-port-forward-frame.ts";

const failureMessage = "Kubernetes port-forward local delivery is incomplete";
const liveStates = new Set(["PREPARED", "ACTIVE"]);
const pendingStates = new Set(["QUEUED", "CLAIMED"]);
const mutationUncertain = (error) =>
  error?.code === "REQUEST_UNKNOWN" ||
  error?.status === 408 ||
  error?.status >= 500;

// Listener открывается только на loopback. Owner client выполняет exact root/session checks; callbacks не назначают connection authority.
// Все writers сериализуются по общему INPUT version; текущий кадр не воспроизводится после неизвестного результата.
export async function createPortForwardBridge({
  client,
  input,
  mappings,
  signal,
}) {
  if (
    !client ||
    !input ||
    !Array.isArray(mappings) ||
    mappings.length < 1 ||
    mappings.length > 16 ||
    mappings.some(
      (m) =>
        !Number.isInteger(m.slot) ||
        m.slot < 0 ||
        m.slot > 15 ||
        !Number.isInteger(m.localPort) ||
        m.localPort < 0 ||
        m.localPort > 65535,
    ) ||
    new Set(mappings.map((m) => m.slot)).size !== mappings.length
  )
    throw Error(failureMessage);
  await client.synchronizeClock(signal);
  const profile = (await client.listConnections(input, signal)).profile;
  if (
    profile.protocol !== "PORT_FORWARD_SPDY_V1" ||
    profile.admissionReasonCode !== "" ||
    profile.maximumActiveConnections !== 16 ||
    mappings.some((m) => m.slot >= profile.ports.length) ||
    client.serverNow() >= Date.parse(input.hardDeadline)
  )
    throw Error(failureMessage);
  const scope = new AbortController(),
    combined = signal ? AbortSignal.any([signal, scope.signal]) : scope.signal;
  const servers = [],
    entries = new Map(),
    allEntries = new Set(),
    sockets = new Set(),
    socketJoins = new Map(),
    tasks = new Set();
  let queue = Promise.resolve(),
    cursor = 0n,
    knownOutputWatermark = 0n,
    problem,
    stopping = false,
    cleanupPromise;
  const operationSignal = () =>
    AbortSignal.any([
      combined,
      AbortSignal.timeout(
        Math.max(
          1,
          Math.floor(
            Math.min(
              30000,
              Date.parse(input.hardDeadline) - client.serverNow(),
            ),
          ),
        ),
      ),
    ]);
  const fail = () => {
    problem ??= Error(failureMessage);
    scope.abort();
    for (const server of servers) server.close();
    for (const socket of sockets) socket.destroy();
  };
  const track = (promise) => {
    tasks.add(promise);
    promise
      .catch(() => {
        if (!stopping && !combined.aborted) fail();
      })
      .finally(() => tasks.delete(promise));
    return promise;
  };
  const serialized = (fn) => {
    const result = queue.then(fn);
    queue = result.catch(() => {});
    return result;
  };
  async function refresh(key, signal = operationSignal()) {
    const result = await client.readInput(input, signal, key);
    input = result.view;
    if (
      !liveStates.has(input.state) ||
      client.serverNow() >= Date.parse(input.hardDeadline)
    )
      throw Error(failureMessage);
    return result;
  }
  async function idle(signal = operationSignal()) {
    await delay(50, undefined, { signal });
  }
  async function ready() {
    while ((await refresh()).view.pending) {
      const state = input.pending.state;
      if (!pendingStates.has(state)) throw Error(failureMessage);
      await idle();
    }
  }
  async function acknowledged(submission, key) {
    while (submission?.state !== "ACKNOWLEDGED") {
      if (!submission || !pendingStates.has(submission.state))
        throw Error(failureMessage);
      await idle();
      const result = await refresh(key);
      submission = result.submission;
    }
    return submission;
  }
  // Только authoritative pre-intent OCC refusal допускает новый If-Match; uncertainty всегда выходит к одному metadata Get.
  async function versionRetry(action, fresh = refresh) {
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        return await action();
      } catch (error) {
        if (
          error?.status !== 409 ||
          error?.code !== "VERSION_MISMATCH" ||
          attempt === 2
        )
          throw error;
        await fresh();
      }
    }
  }
  async function open(entry) {
    await ready();
    const key = randomUUID();
    entry.openKey = key;
    let result;
    try {
      result = await versionRetry(() =>
        client.open(input, entry.slot, key, operationSignal()),
      );
      input = result.input;
    } catch (error) {
      if (!mutationUncertain(error)) throw error;
      entry.openReadAttempted = true;
      result = await client.getConnection(
        input,
        { openIdempotencyKey: key },
        operationSignal(),
      );
      await refresh();
    }
    entry.connection = result.connection;
    entries.set(result.connection.connectionRef, entry);
    await acknowledged(result.submission, key);
    const observed = await client.getConnection(
      input,
      { connectionRef: entry.connection.connectionRef },
      operationSignal(),
    );
    entry.connection = observed.connection;
    if (
      entry.connection.state !== "OPEN" &&
      !(entry.connection.state === "CLOSED" && entry.connection.nativeJoined)
    )
      throw Error(failureMessage);
  }
  async function send(entry, bytes, halfClose = false) {
    await ready();
    if (entry.closed || entry.connection.state !== "OPEN")
      throw Error(failureMessage);
    const key = randomUUID();
    let result;
    try {
      result = await versionRetry(() =>
        client.submit(
          input,
          encodePortForwardInput(entry.connection, bytes, halfClose),
          key,
          operationSignal(),
        ),
      );
      input = result.view;
    } catch (error) {
      if (!mutationUncertain(error)) throw error;
      result = await refresh(key);
    }
    await acknowledged(result.submission, key);
  }
  async function closeOwned(entry, cleanupSignal) {
    if (entry.closeAttempted) return;
    if (!entry.connection && entry.openKey && !entry.openReadAttempted) {
      entry.openReadAttempted = true;
      const recovered = await client.getConnection(
        input,
        { openIdempotencyKey: entry.openKey },
        cleanupSignal,
      );
      entry.connection = recovered.connection;
    }
    if (!entry.connection) throw Error(failureMessage);
    entry.closeAttempted = true;
    const result = await client.readInput(input, cleanupSignal);
    input = result.view;
    const current = await client.getConnection(
      input,
      { connectionRef: entry.connection.connectionRef },
      cleanupSignal,
    );
    entry.connection = current.connection;
    if (["CLOSED", "CANCELLED", "EXPIRED"].includes(entry.connection.state))
      return;
    const key = randomUUID();
    try {
      const reply = await versionRetry(
        () => client.close(input, entry.connection, key, cleanupSignal),
        async () => {
          input = (await client.readInput(input, cleanupSignal)).view;
        },
      );
      input = reply.input;
      entry.connection = reply.connection;
    } catch (error) {
      if (!mutationUncertain(error)) throw error;
      const reply = await client.getConnection(
        input,
        { connectionRef: entry.connection.connectionRef },
        cleanupSignal,
      );
      entry.connection = reply.connection;
      if (!["CLOSED", "CANCELLED", "EXPIRED"].includes(reply.connection.state))
        throw Error(failureMessage);
    }
  }
  async function local(entry) {
    try {
      await serialized(() => open(entry));
      if (entry.connection.state === "CLOSED") return;
      entry.socket.resume();
      for await (const chunk of entry.socket.iterator({
        destroyOnReturn: false,
      })) {
        try {
          if (chunk.length > 65536) throw Error(failureMessage);
          for (
            let offset = 0;
            offset < chunk.length;
            offset += input.maximumFrameBytes - portForwardHeaderBytes
          ) {
            const piece = chunk.subarray(
              offset,
              offset + input.maximumFrameBytes - portForwardHeaderBytes,
            );
            await serialized(() => send(entry, piece));
          }
        } finally {
          chunk.fill(0);
        }
      }
      if (!entry.closed && !combined.aborted)
        await serialized(() => send(entry, new Uint8Array(), true));
    } catch {
      if (!stopping && !entry.closed) {
        problem ??= Error(failureMessage);
        entry.socket.destroy();
      }
    } finally {
      // Local EOF лишь полузакрывает writer. Explicit cleanup выполняется после remote CLOSE либо local error.
      if (entry.socket.destroyed && !stopping) {
        const cleanupSignal = AbortSignal.timeout(5000);
        await serialized(() => closeOwned(entry, cleanupSignal)).catch(() => {
          problem ??= Error(failureMessage);
        });
      }
    }
  }
  async function writeSocket(entry, body, deadline) {
    if (
      client.serverNow() >= deadline ||
      entry.socket.destroyed ||
      !entry.socket.writable
    )
      throw Error(failureMessage);
    await new Promise((resolve, reject) => {
      let settled = false,
        writeUncertain = false;
      const finish = (error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        combined.removeEventListener("abort", aborted);
        entry.socket.off("error", failed);
        entry.socket.off("close", closed);
        error ? reject(Error(failureMessage)) : resolve();
      };
      const failed = () => {
          writeUncertain = true;
          entry.socket.destroy();
        },
        closed = () => finish(true),
        aborted = () => {
          writeUncertain = true;
          entry.socket.destroy();
        };
      const timer = setTimeout(
        aborted,
        Math.max(1, deadline - client.serverNow()),
      );
      combined.addEventListener("abort", aborted, { once: true });
      entry.socket.once("error", failed);
      entry.socket.once("close", closed);
      entry.socket.write(body, (error) =>
        finish(writeUncertain || error || client.serverNow() >= deadline),
      );
      if (combined.aborted) aborted();
    });
  }
  async function output() {
    while (!combined.aborted) {
      const result = await client.readOutput(input, cursor, operationSignal());
      if (
        typeof result.watermark === "bigint" &&
        result.watermark > knownOutputWatermark
      )
        knownOutputWatermark = result.watermark;
      if (result.dropped > 0n) problem ??= Error(failureMessage);
      if (result.status === "WAITING") {
        await idle();
        continue;
      }
      if (result.status !== "FRAME") {
        if (result.status !== "ENDED" || entries.size !== 0)
          throw Error(failureMessage);
        return;
      }
      let frame;
      try {
        frame = decodePortForwardOutput(result.payload, profile.ports.length);
        if (!entries.has(frame.connectionRef)) await queue;
        const entry = entries.get(frame.connectionRef);
        if (
          !entry ||
          BigInt(entry.connection.generation) !== frame.generation ||
          entry.slot !== frame.slot ||
          frame.nativeOrdinal !== entry.ordinal + 1n ||
          client.serverNow() >= result.deadline
        )
          throw Error(failureMessage);
        if (frame.action === "DATA") {
          try {
            await writeSocket(entry, frame.body, result.deadline);
          } catch {
            problem ??= Error(failureMessage);
            entry.socket.destroy();
            await serialized(() =>
              closeOwned(entry, AbortSignal.timeout(5000)),
            ).catch(() => {});
          }
        } else if (frame.action === "HALF_CLOSE") entry.socket.end();
        else {
          entry.closed = true;
          entry.socket.end(() => entry.socket.destroy());
          // Remote pair join и owner closure проверяются отдельно; CLOSE wire не выдаёт remote SUCCESS.
          const observed = await client.getConnection(
            input,
            { connectionRef: entry.connection.connectionRef },
            operationSignal(),
          );
          entry.connection = observed.connection;
          if (!entry.connection.nativeJoined) throw Error(failureMessage);
          entries.delete(frame.connectionRef);
          allEntries.delete(entry);
        }
        entry.ordinal = frame.nativeOrdinal;
        cursor = result.sequence;
      } finally {
        result.payload.fill(0);
        frame?.body.fill(0);
      }
    }
  }
  async function stop() {
    if (cleanupPromise) return cleanupPromise;
    cleanupPromise = (async () => {
      stopping = true;
      scope.abort();
      for (const server of servers) server.close();
      const pendingSocketJoins = [...socketJoins.values()];
      for (const socket of sockets) socket.destroy();
      await Promise.all(pendingSocketJoins);
      await Promise.allSettled([...tasks]);
      const cleanupSignal = AbortSignal.timeout(5000);
      for (const entry of allEntries)
        await closeOwned(entry, cleanupSignal).catch(() => {
          problem ??= Error(failureMessage);
        });
      await Promise.all(
        servers.map(
          (server) => new Promise((resolve) => server.close(() => resolve())),
        ),
      );
      return {
        deliveryComplete: !problem && cursor >= knownOutputWatermark,
        nativeConnectionsJoined: [...allEntries].every(
          (entry) => entry.connection?.nativeJoined === true,
        ),
        afterSequence: String(cursor),
      };
    })();
    return cleanupPromise;
  }
  try {
    for (const mapping of mappings) {
      const server = createServer(
        { allowHalfOpen: true, pauseOnConnect: true, highWaterMark: 32717 },
        (socket) => {
          if (stopping || combined.aborted || sockets.size >= 16) {
            socket.destroy();
            return;
          }
          sockets.add(socket);
          socketJoins.set(
            socket,
            new Promise((resolve) => {
              socket.once("close", () => {
                sockets.delete(socket);
                socketJoins.delete(socket);
                resolve();
              });
            }),
          );
          socket.on("error", () => {});
          const entry = {
            socket,
            slot: mapping.slot,
            ordinal: 0n,
            closed: false,
            closeAttempted: false,
          };
          allEntries.add(entry);
          track(local(entry));
        },
      );
      servers.push(server);
      await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(
          { host: "127.0.0.1", port: mapping.localPort, exclusive: true },
          () => {
            server.off("error", reject);
            server.on("error", fail);
            resolve();
          },
        );
      });
    }
  } catch {
    await stop();
    throw Error(failureMessage);
  }
  const outputTask = track(output());
  const listenerAddresses = servers.map((server, index) => ({
    slot: mappings[index].slot,
    host: "127.0.0.1",
    localPort: server.address().port,
  }));
  return {
    addresses: listenerAddresses,
    stop,
    async joined() {
      await outputTask.catch(() => {});
      return stop();
    },
  };
}
