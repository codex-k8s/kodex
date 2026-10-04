import { mkdir, open, rename, rm, rmdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = fileURLToPath(new URL("..", import.meta.url));
const generatorKinds = new Set(["openapi", "asyncapi", "integration-schema"]);

export async function withFrontendCodegen(kind, generate, root = frontendRoot) {
  if (!generatorKinds.has(kind) || typeof generate !== "function")
    throw new Error("Invalid frontend code generation operation");
  const directory = resolve(root, ".kodex-codegen", kind);
  await mkdir(directory, { recursive: true });
  // Один generator владеет output; FAILED разрешает только явный новый запуск.
  let claim;
  try {
    claim = await open(resolve(directory, "active"), "wx", 0o600);
  } catch {
    throw new Error(
      "Frontend code generation is already active or was interrupted; inspect its marker before retrying",
    );
  }
  const startedAt = Date.now();
  const statusPath = resolve(directory, "status.json");
  async function status(state) {
    const temporary = resolve(directory, "status.pending");
    await writeFile(temporary, JSON.stringify({ state, startedAt }), {
      mode: 0o600,
    });
    await rename(temporary, statusPath);
  }
  async function release() {
    await claim.close();
    await rm(resolve(directory, "active"));
  }
  let result;
  try {
    await status("RUNNING");
    result = await generate();
    await rm(statusPath);
  } catch (error) {
    await status("FAILED");
    await release();
    throw error;
  }
  await release();
  try {
    await rmdir(directory);
  } catch (error) {
    // Новый владелец мог начать работу после release прежнего claim.
    if (error.code !== "ENOTEMPTY") throw error;
  }
  return result;
}
