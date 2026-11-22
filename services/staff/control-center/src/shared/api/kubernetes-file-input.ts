import {
  getManagedKubernetesFileInput,
  listManagedKubernetesFileInputs,
  repairManagedKubernetesFileCleanup,
} from "./generated/openapi/sdk.gen";
import {
  parseKubernetesFileSourceReadback,
  parseKubernetesFileCleanupList,
  parseKubernetesFileCleanupReadback,
  parseKubernetesFileCleanupView,
  parseKubernetesFileInputList,
  validKubernetesFileInvocation,
  validKubernetesFileSource,
  KubernetesFileProtocolError,
} from "@/shared/lib/kubernetes-file-input";
import { assertOwnerRequest } from "./owner-lifetime";
import { requestSignal } from "./client";
import { mutate, etag } from "./mutation";
import type { KubernetesFileCleanupView } from "./generated/openapi/types.gen";
import { unwrap } from "./problem";
// Только authenticated metadata GET; source, bytes и execution credit назначает owner.
export async function listKubernetesFileInputs(
  invocationRef: string,
  signal: AbortSignal,
) {
  assertOwnerRequest(signal);
  if (!validKubernetesFileInvocation(invocationRef))
    throw new KubernetesFileProtocolError();
  const response = await unwrap(
    listManagedKubernetesFileInputs({
      path: { invocationRef },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return parseKubernetesFileInputList(response.data, invocationRef);
}
export async function readKubernetesFileInput(
  invocationRef: string,
  sourceRef: string,
  signal: AbortSignal,
) {
  assertOwnerRequest(signal);
  if (
    !validKubernetesFileInvocation(invocationRef) ||
    !validKubernetesFileSource(sourceRef)
  )
    throw new KubernetesFileProtocolError();
  const response = await unwrap(
    getManagedKubernetesFileInput({
      path: { invocationRef, sourceRef },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return parseKubernetesFileSourceReadback(
    response.data,
    invocationRef,
    sourceRef,
  );
}

// Closure metadata разрешает owner без source bytes: artifact.view не подменяется локальной догадкой.
export async function listKubernetesFileCleanup(
  invocationRef: string,
  signal: AbortSignal,
) {
  assertOwnerRequest(signal);
  if (!validKubernetesFileInvocation(invocationRef))
    throw new KubernetesFileProtocolError();
  const response = await unwrap(
    listManagedKubernetesFileInputs({
      path: { invocationRef },
      query: { cleanupOnly: true },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  return parseKubernetesFileCleanupList(response.data, invocationRef);
}
export async function readKubernetesFileCleanup(
  invocationRef: string,
  sourceRef: string,
  signal: AbortSignal,
  key = "",
) {
  assertOwnerRequest(signal);
  if (
    !validKubernetesFileInvocation(invocationRef) ||
    !validKubernetesFileSource(sourceRef) ||
    (key !== "" && !/^[A-Za-z0-9_.:-]{8,128}$/u.test(key))
  )
    throw new KubernetesFileProtocolError();
  const response = await unwrap(
    getManagedKubernetesFileInput({
      path: { invocationRef, sourceRef },
      query: {
        cleanupOnly: true,
        ...(key ? { cleanupIdempotencyKey: key } : {}),
      },
      signal: requestSignal(signal),
    }),
  );
  assertOwnerRequest(signal);
  const result = parseKubernetesFileCleanupReadback(
    response.data,
    invocationRef,
    sourceRef,
  );
  if (!key && result.cleanupReceipt) throw new KubernetesFileProtocolError();
  return result;
}
// Один POST начинает технический cleanup cycle. Потерянный ACK сверяется отдельно по исходному key.
export async function repairKubernetesFileCleanup(
  invocationRef: string,
  selected: KubernetesFileCleanupView,
  signal: AbortSignal,
  key: string,
) {
  assertOwnerRequest(signal);
  parseKubernetesFileCleanupView(selected, invocationRef, selected.sourceRef);
  if (
    selected.cleanup.eligibleExhaustedCount !== 1 ||
    !/^[A-Za-z0-9_.:-]{8,128}$/u.test(key)
  )
    throw new KubernetesFileProtocolError();
  const response = await mutate(
    (headers) =>
      repairManagedKubernetesFileCleanup({
        path: { invocationRef, sourceRef: selected.sourceRef },
        body: {},
        headers: { ...headers, "If-Match": etag(selected.cleanup.version) },
        signal: requestSignal(signal),
      }),
    selected.cleanup.version,
    key,
  );
  assertOwnerRequest(signal);
  return parseKubernetesFileCleanupReadback(
    response.data,
    invocationRef,
    selected.sourceRef,
    true,
  );
}
