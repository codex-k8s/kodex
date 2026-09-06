import { requestSignal } from "@/shared/api/client";
import {
  deleteArtifact,
  getArtifactImpact,
  listArtifacts,
  purgeArtifact,
  restoreArtifact,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Artifact,
  ArtifactImpact,
  ArtifactPurgeReceipt,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import { runtimeConfig } from "@/shared/config/runtime";
import { currentLocale } from "@/shared/locale";
import type {
  AsyncEntityLoadRequest,
  AsyncEntityPage,
  AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";

export interface ArtifactListItem extends AsyncEntityPickerItem {
  artifact: Artifact;
}

export interface ArtifactBulkReceipt {
  artifact: Artifact;
  problem?: AppProblem;
  status: "SUCCEEDED" | "FAILED";
}

interface ArtifactListBaseFilters {
  lifecycleState?: Artifact["lifecycleState"];
  scanState?: Artifact["scanState"];
  type?: "TEXT" | "DOCUMENT" | "IMAGE";
}

export type ArtifactListFilters = ArtifactListBaseFilters &
  (
    | { allSources: true; sourceKinds?: never }
    | { allSources?: false; sourceKinds: readonly Artifact["source"][] }
  );

export interface ArtifactUploadRequest {
  signal: AbortSignal;
  onProgress: (progress: { loadedBytes: number; totalBytes: number }) => void;
}

interface GeneratedResponse<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

const artifactPageSize = 40;

function responseHeaders(xhr: XMLHttpRequest): Headers {
  const headers = new Headers();
  for (const line of xhr
    .getAllResponseHeaders()
    .trim()
    .split(/[\r\n]+/)) {
    if (!line) continue;
    const separator = line.indexOf(":");
    if (separator < 1) continue;
    headers.append(
      line.slice(0, separator).trim(),
      line.slice(separator + 1).trim(),
    );
  }
  return headers;
}

function parseResponseBody(xhr: XMLHttpRequest): unknown {
  if (!xhr.responseText) return undefined;
  try {
    return JSON.parse(xhr.responseText);
  } catch {
    return undefined;
  }
}

function utf8HeaderValue(value: string): string {
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint <= 31 || codePoint === 127)
      throw new Error("Artifact file name contains unsupported characters");
  }
  return Array.from(new TextEncoder().encode(value), (byte) =>
    String.fromCharCode(byte),
  ).join("");
}

function uploadArtifactRequest(
  projectRef: string,
  file: File,
  headers: MutationHeaders,
  request: ArtifactUploadRequest,
): Promise<GeneratedResponse<Artifact>> {
  return new Promise((resolve, reject) => {
    if (request.signal.aborted) {
      reject(new DOMException("Artifact upload was cancelled", "AbortError"));
      return;
    }
    const xhr = new XMLHttpRequest();
    const path = `/api/v1/projects/${encodeURIComponent(projectRef)}/artifacts`;
    const cleanup = () => request.signal.removeEventListener("abort", abort);
    const abort = () => xhr.abort();
    xhr.open(
      "POST",
      new URL(path, `${runtimeConfig().apiBaseUrl}/`).toString(),
    );
    xhr.withCredentials = true;
    xhr.timeout = runtimeConfig().requestTimeoutMs;
    xhr.setRequestHeader("Accept", "application/json");
    xhr.setRequestHeader("Accept-Language", currentLocale());
    xhr.setRequestHeader(
      "Content-Type",
      file.type || "application/octet-stream",
    );
    xhr.setRequestHeader("Idempotency-Key", headers["Idempotency-Key"]);
    xhr.setRequestHeader("X-CSRF-Token", headers["X-CSRF-Token"]);
    xhr.setRequestHeader("X-File-Name", utf8HeaderValue(file.name));
    xhr.setRequestHeader("X-Kodex-Project-ID", projectRef);
    xhr.upload.addEventListener("progress", (event) => {
      request.onProgress({
        loadedBytes: Math.min(event.loaded, file.size),
        totalBytes: file.size,
      });
    });
    xhr.onload = () => {
      cleanup();
      const payload = parseResponseBody(xhr);
      const response = new Response(null, {
        headers: responseHeaders(xhr),
        status: xhr.status,
        statusText: xhr.statusText,
      });
      resolve(
        response.ok && typeof payload === "object" && payload !== null
          ? { data: payload as Artifact, response }
          : {
              error: payload ?? {
                code: "ARTIFACT_UPLOAD_INVALID_RESPONSE",
                retryable: true,
                status: xhr.status,
              },
              response,
            },
      );
    };
    xhr.onerror = () => {
      cleanup();
      resolve({
        error: { code: "ARTIFACT_UPLOAD_FAILED", retryable: true, status: 0 },
      });
    };
    xhr.ontimeout = xhr.onerror;
    xhr.onabort = () => {
      cleanup();
      reject(new DOMException("Artifact upload was cancelled", "AbortError"));
    };
    request.signal.addEventListener("abort", abort, { once: true });
    request.onProgress({ loadedBytes: 0, totalBytes: file.size });
    xhr.send(file);
  });
}

export async function mutateArtifactsSequentially(
  artifacts: readonly Artifact[],
  command: (artifact: Artifact) => Promise<unknown>,
): Promise<ArtifactBulkReceipt[]> {
  const receipts: ArtifactBulkReceipt[] = [];
  for (const artifact of artifacts) {
    try {
      await command(artifact);
      receipts.push({ artifact, status: "SUCCEEDED" });
    } catch (error) {
      receipts.push({ artifact, problem: asProblem(error), status: "FAILED" });
    }
  }
  return receipts;
}

export async function loadArtifactPage(
  projectRef: string,
  request: AsyncEntityLoadRequest,
  filters: ArtifactListFilters,
): Promise<AsyncEntityPage<ArtifactListItem>> {
  const query = request.query.trim();
  const sourceKinds = filters.allSources
    ? undefined
    : [...new Set(filters.sourceKinds)];
  if (sourceKinds?.length === 0)
    return { items: [], total: 0, nextCursor: null };
  const result = await unwrap(
    listArtifacts({
      path: { projectRef },
      query: {
        lifecycleState: filters.lifecycleState ?? "ACTIVE",
        pageSize: artifactPageSize,
        ...(sourceKinds ? { sourceKinds } : {}),
        ...(filters.type ? { type: filters.type } : {}),
        ...(filters.scanState ? { scanState: filters.scanState } : {}),
        ...(query ? { query } : {}),
        ...(request.cursor ? { pageToken: request.cursor } : {}),
      },
      signal: requestSignal(request.signal),
    }),
  );
  request.signal.throwIfAborted();
  if (
    !Number.isSafeInteger(result.data.total) ||
    result.data.total < result.data.items.length
  )
    throw new Error("Invalid artifact catalog total");
  return {
    items: result.data.items.map((artifact) => ({
      artifact,
      description: artifact.mediaType,
      id: artifact.ref,
      label: artifact.fileName,
    })),
    total: result.data.total,
    nextCursor: result.data.nextPageToken ?? null,
  };
}

export async function uploadArtifactItem(
  projectRef: string,
  file: File,
  request: ArtifactUploadRequest,
): Promise<Artifact> {
  return (
    await mutate((headers) =>
      uploadArtifactRequest(projectRef, file, headers, request),
    )
  ).data;
}

export async function loadArtifactImpact(
  artifact: Pick<Artifact, "ref" | "version">,
  action: ArtifactImpact["action"],
  signal?: AbortSignal,
): Promise<ArtifactImpact> {
  const impact = (
    await unwrap(
      getArtifactImpact({
        path: { artifactRef: artifact.ref },
        query: { action },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    impact.artifactRef !== artifact.ref ||
    impact.artifactVersion !== artifact.version ||
    impact.action !== action
  )
    throw new Error("Artifact impact does not match the requested revision");
  return impact;
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "If-Match": string;
  "X-CSRF-Token": string;
} {
  if (!headers["If-Match"])
    throw new Error("Artifact version header is unavailable");
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "If-Match": headers["If-Match"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

function destructiveHeaders(
  headers: MutationHeaders,
  artifact: Pick<Artifact, "ref" | "version">,
  impact: ArtifactImpact,
  action: ArtifactImpact["action"],
): ReturnType<typeof versionedHeaders> & { "X-Impact-Digest": string } {
  if (
    !impact.permitted ||
    impact.action !== action ||
    impact.artifactRef !== artifact.ref ||
    impact.artifactVersion !== artifact.version
  )
    throw new Error("Artifact impact does not authorize this mutation");
  return {
    ...versionedHeaders(headers),
    "X-Impact-Digest": impact.impactDigest,
  };
}

export async function deleteArtifactItem(
  artifact: Pick<Artifact, "ref" | "version">,
  impact: ArtifactImpact,
  signal?: AbortSignal,
): Promise<Artifact> {
  return (
    await mutate(
      (headers) =>
        deleteArtifact({
          path: { artifactRef: artifact.ref },
          headers: destructiveHeaders(headers, artifact, impact, "DELETE"),
          signal: requestSignal(signal),
        }),
      artifact.version,
    )
  ).data;
}

export async function restoreArtifactItem(
  artifact: Pick<Artifact, "ref" | "version">,
  signal?: AbortSignal,
): Promise<Artifact> {
  return (
    await mutate(
      (headers) =>
        restoreArtifact({
          path: { artifactRef: artifact.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(signal),
        }),
      artifact.version,
    )
  ).data;
}

export async function purgeArtifactItem(
  artifact: Pick<Artifact, "ref" | "version">,
  impact: ArtifactImpact,
  signal?: AbortSignal,
): Promise<ArtifactPurgeReceipt> {
  return (
    await mutate(
      (headers) =>
        purgeArtifact({
          path: { artifactRef: artifact.ref },
          headers: destructiveHeaders(headers, artifact, impact, "PURGE"),
          signal: requestSignal(signal),
        }),
      artifact.version,
    )
  ).data;
}
