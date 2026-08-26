import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";

const publicPath = new URL(
  "../public/config/runtime-config.json",
  import.meta.url,
);
const deploymentPath = new URL(
  "../../../../deploy/k8s/base/staff-control-center/runtime-config.json",
  import.meta.url,
);
const headersPath = new URL(
  "../../../../deploy/k8s/base/staff-control-center/security-headers.conf",
  import.meta.url,
);
const readinessPath = new URL(
  "../../../../deploy/k8s/base/staff-control-center/readiness.json",
  import.meta.url,
);
const podTemplatePath = new URL(
  "../../../../deploy/k8s/base/staff-control-center/deployment.yaml",
  import.meta.url,
);

const [
  publicText,
  deploymentText,
  headersText,
  readinessText,
  podTemplateText,
] = await Promise.all([
  readFile(publicPath, "utf8"),
  readFile(deploymentPath, "utf8"),
  readFile(headersPath, "utf8"),
  readFile(readinessPath, "utf8"),
  readFile(podTemplatePath, "utf8"),
]);
const publicConfig = JSON.parse(publicText);
const deploymentConfig = JSON.parse(deploymentText);
if (JSON.stringify(publicConfig) !== JSON.stringify(deploymentConfig))
  throw new Error("Runtime config copies differ");
const revision = deploymentConfig.revision;
delete deploymentConfig.revision;
const expected = createHash("sha256")
  .update(JSON.stringify(deploymentConfig))
  .update("\0")
  .update(`${headersText.trimEnd()}\n`)
  .digest("hex");
if (revision !== expected) throw new Error("Runtime config revision mismatch");
const readiness = JSON.parse(readinessText);
const hsts =
  'add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;';
if (!headersText.split("\n").includes(hsts))
  throw new Error("Production HSTS header is absent or weaker than required");
if (
  readiness.runtimeConfigRevision !== expected ||
  readiness.tlsMode !== "mutual-verified"
)
  throw new Error("Readiness revision or TLS mode mismatch");
const annotation = `kodex.dev/runtime-config-revision: "${expected}"`;
if (!podTemplateText.includes(annotation))
  throw new Error("Pod template runtime config revision mismatch");
process.stdout.write(`Runtime config revision verified: ${expected}\n`);
