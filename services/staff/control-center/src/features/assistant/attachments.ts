import type { Artifact } from "@/shared/api/generated/openapi/types.gen";

export interface WaitForCleanArtifactOptions {
  signal: AbortSignal;
  read: (artifactRef: string) => Promise<Artifact>;
  intervalMs?: number;
  maxAttempts?: number;
}

function abortError(): DOMException {
  return new DOMException("Attachment scan was cancelled", "AbortError");
}

async function wait(intervalMs: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) throw abortError();
  await new Promise<void>((resolve, reject) => {
    const handleAbort = () => {
      globalThis.clearTimeout(timeout);
      signal.removeEventListener("abort", handleAbort);
      reject(abortError());
    };
    const timeout = globalThis.setTimeout(() => {
      signal.removeEventListener("abort", handleAbort);
      resolve();
    }, intervalMs);
    signal.addEventListener("abort", handleAbort, { once: true });
  });
}

export async function waitForCleanArtifact(
  initial: Artifact,
  options: WaitForCleanArtifactOptions,
): Promise<Artifact> {
  const intervalMs = options.intervalMs ?? 1_000;
  const maxAttempts = options.maxAttempts ?? 120;
  let artifact = initial;

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    if (options.signal.aborted) throw abortError();
    if (artifact.lifecycleState === "ACTIVE" && artifact.scanState === "CLEAN")
      return artifact;
    if (
      artifact.lifecycleState !== "ACTIVE" ||
      artifact.scanState === "QUARANTINED" ||
      artifact.scanState === "FAILED"
    )
      throw new Error(`Attachment is not safe to use: ${artifact.scanState}`);

    await wait(intervalMs, options.signal);
    artifact = await options.read(artifact.ref);
  }

  throw new Error("Attachment scan did not complete in time");
}
