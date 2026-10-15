import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { validateStableReceipt } from "./stable-image-publication.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
const source = path => readFileSync(new URL(path, new URL("../../", import.meta.url)), "utf8");

test("registry helper has a closed standalone nonroot target and normal render binding", () => {
  const recipe = JSON.parse(source("tools/release/images.json")).images.find(image => image.component === "registry-pull-authorizer");
  assert.deepEqual(recipe, { component: "registry-pull-authorizer", dockerfile: "services/jobs/role-image-builder/Dockerfile",
    target: "registry-pull-runtime" });
  const dockerfile = source(recipe.dockerfile), runtime = dockerfile.split(" AS registry-pull-runtime\n")[1];
  assert.ok(runtime); assert.equal(runtime.includes("FROM "), false);
  assert.equal(runtime.match(/^COPY /gm)?.length, 1);
  assert.match(runtime, /USER 10001:10001/);
  assert.match(runtime, /ENTRYPOINT \["\/usr\/local\/bin\/registry-pull-authorizer"\]/);
  const yaml = execFileSync("kubectl", ["kustomize", root + "deploy/k8s/profiles/web-only"], { encoding: "utf8", maxBuffer: 16 << 20 });
  const rendered = execFileSync("yq", ["-N", "-o=json", "-I=0", ".", "-"], { input: yaml, encoding: "utf8", maxBuffer: 16 << 20 });
  const deployment = rendered.trim().split("\n").map(JSON.parse).find(r => r.kind === "Deployment" && r.metadata.name === "kodex-image-registry-pull");
  const helper = deployment.spec.template.spec.containers.find(c => c.name === "pull-authorizer");
  assert.match(helper.image, /\/registry-pull-authorizer@sha256:/);
  assert.deepEqual(helper.command, ["/usr/local/bin/registry-pull-authorizer"]);
  assert.equal(helper.volumeMounts.find(m => m.name === "credentials").readOnly, true);
  assert.equal(helper.securityContext.runAsUser, 10001);
  assert.equal(validateStableReceipt({ schema: "kodex.stable-local-build/v1", component: recipe.component,
    sourceSHA: "a".repeat(40), contextSHA256: "b".repeat(64), archiveSHA256: "c".repeat(64), imageDigest: "sha256:" + "d".repeat(64),
    admission: "NOT_RUN", publication: "NOT_RUN", nodePull: "NOT_RUN" }).component, recipe.component);
});

test("actual local renderer expression keeps its explicit admission bundle without changing unrelated images", () => {
  const renderer = source("tools/dev/render-local.sh");
  const marker = 'select(.image | test("/(image-admission|registry-pull-authorizer)@sha256:"));';
  const index = renderer.indexOf(marker);
  assert.notEqual(index, -1);
  const start = renderer.lastIndexOf("with((.spec.template.spec.initContainers[]?, .spec.template.spec.containers[]?) |", index);
  const end = renderer.indexOf("\n    ) |", index);
  assert.ok(start > 0 && end > index);
  const expression = renderer.slice(start, end + "\n    )".length);
  const fixture = { spec: { template: { spec: { containers: ["image-admission", "registry-pull-authorizer", "unrelated"].map(name => ({
    name, image: `fixture.invalid/kodex/${name}@sha256:${"a".repeat(64)}` })) } } } };
  const bundle = "fixture.invalid/kodex/image-admission@sha256:" + "b".repeat(64);
  const output = execFileSync("yq", ["-o=json", expression, "-"], { input: JSON.stringify(fixture), encoding: "utf8",
    env: { PATH: process.env.PATH, IMAGE_ADMISSION_IMAGE: bundle } });
  const containers = JSON.parse(output).spec.template.spec.containers;
  assert.equal(containers[0].image, bundle); assert.equal(containers[1].image, bundle);
  assert.equal(containers[2].image, fixture.spec.template.spec.containers[2].image);
});
