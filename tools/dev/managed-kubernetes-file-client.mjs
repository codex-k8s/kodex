import { createOwnerRuntimeSessionClient } from "./owner-session-client.mjs";
import { boundedResponseBody } from "./runtime-workspace-acceptance.mjs";
import {
  parseKubernetesFileSourceReadback,
  parseKubernetesFileCleanupList,
  parseKubernetesFileCleanupReadback,
  parseKubernetesFileInputList,
  validKubernetesFileInvocation,
  validKubernetesFileSource,
} from "../../services/staff/control-center/src/shared/lib/kubernetes-file-input.ts";

const metadataFailure = "Kubernetes file input metadata request failed";
// Только metadata GET через прежний session/renew consumer. Source bytes, revision и INPUT не выбираются CLI.
export function createKubernetesFileOwnerClient(options) {
  const session = createOwnerRuntimeSessionClient(options);
  async function read(
    invocationRef,
    sourceRef,
    signal,
    cleanupOnly = false,
    key = "",
  ) {
    if (
      !validKubernetesFileInvocation(invocationRef) ||
      (sourceRef !== undefined && !validKubernetesFileSource(sourceRef)) ||
      !(signal instanceof AbortSignal) ||
      signal.aborted
    )
      throw Error(metadataFailure);
    if (key !== "" && (!cleanupOnly || !/^[A-Za-z0-9_.:-]{8,128}$/u.test(key)))
      throw Error(metadataFailure);
    const path = `/api/v1/integration-invocations/${invocationRef}/kubernetes-file-inputs${sourceRef === undefined ? "" : "/" + sourceRef}${cleanupOnly ? "?cleanupOnly=true" + (key ? "&cleanupIdempotencyKey=" + encodeURIComponent(key) : "") : ""}`;
    let raw;
    try {
      const response = await session.request(path, {
        method: "GET",
        signal,
        headers: { Accept: "application/json" },
      });
      if (
        response.status !== 200 ||
        response.headers.get("content-type")?.split(";")[0].trim() !==
          "application/json" ||
        !response.headers
          .get("cache-control")
          ?.split(",")
          .some((v) => v.trim().toLowerCase() === "no-store")
      ) {
        await response.body?.cancel();
        throw Error(metadataFailure);
      }
      raw = await boundedResponseBody(
        response,
        sourceRef === undefined ? 512 * 1024 : 4096,
      );
      if (signal.aborted) throw Error(metadataFailure);
      const value = JSON.parse(
        new TextDecoder("utf-8", { fatal: true }).decode(raw),
      );
      if (cleanupOnly) {
        if (sourceRef === undefined)
          return parseKubernetesFileCleanupList(value, invocationRef);
        const result = parseKubernetesFileCleanupReadback(
          value,
          invocationRef,
          sourceRef,
        );
        if (!key && result.cleanupReceipt) throw Error(metadataFailure);
        return result;
      }
      return sourceRef === undefined
        ? parseKubernetesFileInputList(value, invocationRef)
        : parseKubernetesFileSourceReadback(value, invocationRef, sourceRef);
    } catch {
      throw Error(metadataFailure);
    } finally {
      raw?.fill(0);
    }
  }
  return {
    listCleanup(invocationRef, signal) {
      return read(invocationRef, undefined, signal, true);
    },
    getCleanup(invocationRef, sourceRef, signal, key = "") {
      return read(invocationRef, sourceRef, signal, true, key);
    },
    // Сначала читаем доступность команды, затем отправляем один POST.
    // Неизвестный ответ допускает один GET с исходным ключом.
    async repairCleanup(invocationRef, sourceRef, version, key, signal) {
      if (
        !validKubernetesFileInvocation(invocationRef) ||
        !validKubernetesFileSource(sourceRef) ||
        !Number.isSafeInteger(version) ||
        version < 1 ||
        !/^[A-Za-z0-9_.:-]{8,128}$/u.test(key) ||
        !(signal instanceof AbortSignal) ||
        signal.aborted
      )
        throw Error(metadataFailure);
      const current = await read(invocationRef, sourceRef, signal, true);
      if (current.cleanupView.repairAvailable !== true)
        throw Error(metadataFailure);
      let raw;
      try {
        const response = await session.request(
          `/api/v1/integration-invocations/${invocationRef}/kubernetes-file-inputs/${sourceRef}/cleanup-repair`,
          {
            method: "POST",
            signal,
            headers: {
              "Content-Type": "application/json",
              "If-Match": `"${version}"`,
              "Idempotency-Key": key,
            },
            body: "{}",
          },
        );
        if (
          response.status !== 200 ||
          response.headers.get("content-type")?.split(";")[0].trim() !==
            "application/json" ||
          !response.headers
            .get("cache-control")
            ?.split(",")
            .some((value) => value.trim().toLowerCase() === "no-store")
        ) {
          await response.body?.cancel();
          throw Error(metadataFailure);
        }
        raw = await boundedResponseBody(response, 4096);
        if (signal.aborted) throw Error(metadataFailure);
        return parseKubernetesFileCleanupReadback(
          JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(raw)),
          invocationRef,
          sourceRef,
          true,
        );
      } catch {
        if (signal.aborted) throw Error(metadataFailure);
        const result = await read(invocationRef, sourceRef, signal, true, key);
        if (!result.cleanupReceipt) throw Error(metadataFailure);
        return result;
      } finally {
        raw?.fill(0);
      }
    },
    list(invocationRef, signal) {
      return read(invocationRef, undefined, signal);
    },
    get(invocationRef, sourceRef, signal) {
      return read(invocationRef, sourceRef, signal);
    },
  };
}
