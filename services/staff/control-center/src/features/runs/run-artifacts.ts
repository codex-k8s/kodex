import type { Run, RunGraph } from "@/shared/api/generated/openapi/types.gen";

export function runArtifactReferences(run: Run, graph: RunGraph): string[] {
  if (graph.runRef !== run.rootRunRef && graph.runRef !== run.ref)
    throw new Error("Run artifact graph identity mismatch");
  const refs = [
    ...run.artifactRefs,
    ...graph.nodes.flatMap((node) => node.artifactRefs),
  ];
  if (refs.some((ref) => !/^[A-Za-z0-9_-]{8,128}$/.test(ref)))
    throw new Error("Run artifact reference is invalid");
  return [...new Set(refs)];
}

// Ограничивается число одновременных чтений, но не теряются ссылки снимка.
export async function hydrateRunArtifacts(
  refs: readonly string[],
  read: (ref: string) => Promise<unknown>,
  signal: AbortSignal,
): Promise<void> {
  let cursor = 0;
  async function worker(): Promise<void> {
    while (cursor < refs.length) {
      signal.throwIfAborted();
      const ref = refs[cursor++];
      if (ref) await read(ref);
      signal.throwIfAborted();
    }
  }
  await Promise.all(Array.from({ length: Math.min(4, refs.length) }, worker));
}
