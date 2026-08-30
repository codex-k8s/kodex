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

export interface ArtifactListFilters {
  lifecycleState?: Artifact["lifecycleState"];
  scanState?: Artifact["scanState"];
  sourceKinds: readonly Artifact["source"][];
  type?: "TEXT" | "DOCUMENT" | "IMAGE";
}

export interface ArtifactUploadRequest {
  signal: AbortSignal;
  onProgress: (progress: { loadedBytes: number; totalBytes: number }) => void;
}

interface ArtifactCursorState {
  version: 1;
  sources: Partial<Record<Artifact["source"], string | null>>;
}

interface GeneratedResponse<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

const artifactPageSize = 40;

function parseCursor(
  cursor: string | undefined,
  sources: readonly Artifact["source"][],
): Partial<Record<Artifact["source"], string | null>> {
  if (!cursor) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(cursor);
  } catch {
    throw new Error("Artifact cursor is invalid");
  }
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    !("version" in parsed) ||
    parsed.version !== 1 ||
    !("sources" in parsed) ||
    typeof parsed.sources !== "object" ||
    parsed.sources === null
  )
    throw new Error("Artifact cursor is invalid");
  const values = parsed.sources as Record<string, unknown>;
  const result: Partial<Record<Artifact["source"], string | null>> = {};
  for (const source of sources) {
    const value = values[source];
    if (value !== undefined && value !== null && typeof value !== "string")
      throw new Error("Artifact cursor is invalid");
    if (value === null || typeof value === "string") result[source] = value;
  }
  return result;
}

function serializeCursor(
  sources: Partial<Record<Artifact["source"], string | null>>,
): string | null {
  if (Object.values(sources).every((value) => value === null)) return null;
  return JSON.stringify({ version: 1, sources } satisfies ArtifactCursorState);
}

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
  const sourceKinds = [...new Set(filters.sourceKinds)];
  if (sourceKinds.length === 0) return { items: [], nextCursor: null };
  const cursors = parseCursor(request.cursor, sourceKinds);
  const activeSourceCount = sourceKinds.filter(
    (sourceKind) => cursors[sourceKind] !== null,
  ).length;
  const pageSize = Math.max(
    1,
    Math.floor(artifactPageSize / Math.max(1, activeSourceCount)),
  );
  const pages = await Promise.all(
    sourceKinds.map(async (sourceKind) => {
      const cursor = cursors[sourceKind];
      if (cursor === null)
        return {
          items: [] as Artifact[],
          nextPageToken: undefined,
          sourceKind,
        };
      const result = await unwrap(
        listArtifacts({
          path: { projectRef },
          query: {
            lifecycleState: filters.lifecycleState ?? "ACTIVE",
            pageSize,
            sourceKind,
            ...(filters.type ? { type: filters.type } : {}),
            ...(filters.scanState ? { scanState: filters.scanState } : {}),
            ...(query ? { query } : {}),
            ...(cursor ? { pageToken: cursor } : {}),
          },
          signal: request.signal,
        }),
      );
      return {
        items: result.data.items,
        nextPageToken: result.data.nextPageToken,
        sourceKind,
      };
    }),
  );
  const nextSources: Partial<Record<Artifact["source"], string | null>> = {};
  for (const page of pages)
    nextSources[page.sourceKind] = page.nextPageToken ?? null;
  const artifacts = pages
    .flatMap((page) => page.items)
    .sort(
      (left, right) =>
        right.createdAt.localeCompare(left.createdAt) ||
        left.ref.localeCompare(right.ref),
    );
  return {
    items: artifacts.map((artifact) => ({
      artifact,
      description: artifact.mediaType,
      id: artifact.ref,
      label: artifact.fileName,
    })),
    nextCursor: serializeCursor(nextSources),
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
  artifact: Artifact,
  action: ArtifactImpact["action"],
): Promise<ArtifactImpact> {
  const impact = (
    await unwrap(
      getArtifactImpact({
        path: { artifactRef: artifact.ref },
        query: { action },
        signal: requestSignal(),
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
  artifact: Artifact,
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
  artifact: Artifact,
  impact: ArtifactImpact,
): Promise<Artifact> {
  return (
    await mutate(
      (headers) =>
        deleteArtifact({
          path: { artifactRef: artifact.ref },
          headers: destructiveHeaders(headers, artifact, impact, "DELETE"),
          signal: requestSignal(),
        }),
      artifact.version,
    )
  ).data;
}

export async function restoreArtifactItem(
  artifact: Artifact,
): Promise<Artifact> {
  return (
    await mutate(
      (headers) =>
        restoreArtifact({
          path: { artifactRef: artifact.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      artifact.version,
    )
  ).data;
}

export async function purgeArtifactItem(
  artifact: Artifact,
  impact: ArtifactImpact,
): Promise<ArtifactPurgeReceipt> {
  return (
    await mutate(
      (headers) =>
        purgeArtifact({
          path: { artifactRef: artifact.ref },
          headers: destructiveHeaders(headers, artifact, impact, "PURGE"),
          signal: requestSignal(),
        }),
      artifact.version,
    )
  ).data;
}
