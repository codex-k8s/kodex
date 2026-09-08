import assert from "node:assert/strict";
import { createHash } from "node:crypto";
const h = (value) => createHash("sha256").update(value).digest("hex");
const digest = "a".repeat(64);
const response = (value, status = 200) => new Response(JSON.stringify(value), { status });

export function artifactDetail(generation = 1) {
  const artifact = { ref: `artifact${generation}`, recipeRef: "recipe", recipeGeneration: generation, manifestDigest: `sha256:${digest}`, provenanceSha256: digest, sbomSha256: digest, vulnerabilityEvidenceSha256: digest, admissionVerdict: "ACCEPTED", promotedReference: `pull.fixture.invalid/kodex/roles@sha256:${digest}`, promotionReceiptSha256: digest, promotedAt: new Date().toISOString() };
  return { recipe: { ref: "recipe", version: generation, generation, managedLineage: { managedBy: "UI", configurationRef: "configuration", revisionRef: `revision${generation}` }, promotedImageReady: true, activeImageArtifactRef: artifact.ref }, builds: [{ ref: `build${generation}`, recipeRef: "recipe", recipeGeneration: generation, configurationRevisionRef: `revision${generation}`, stage: "COMPLETED" }], promotionCandidate: artifact, activeArtifact: artifact };
}

export function roleImageTransportFixture() {
  const revisions = []; let generation = 0; let configurationVersion = 1; let bound = false; let activeReference; let activeArtifactRef; let publishedRevision; let mutations = 0;
  const template = `FROM registry.fixture.invalid/kodex/agent-runner@sha256:${digest}\n`;
  const fixtureName = () => revisions.length ? JSON.parse(revisions.at(-1).content).name : undefined;
  const configuration = () => ({ name: fixtureName(), ref: "configuration", version: configurationVersion, kind: "ROLE_IMAGE", managedBy: "UI", projectRef: "project", ...(publishedRevision ? { currentRevision: publishedRevision } : {}) });
  const request = async (path, options = {}) => {
    const method = options.method ?? "GET"; const url = new URL(path, "https://fixture.invalid"); const body = options.body ? JSON.parse(options.body) : undefined;
    if (method !== "GET") mutations++;
    if (method === "GET" && url.pathname === "/api/v1/projects") return response({ items: [] });
    if (method === "GET" && url.pathname === "/api/v1/role-environments") return response({ items: [{ key: "standard", available: true, dockerfileTemplate: template }] });
    if (method === "POST" && path === "/api/v1/projects") return response({ ref: "project" }, 201);
    if (method === "POST" && path.endsWith("/agents")) return response({ ref: "agent", roleDefinitionRef: "role", version: 1 }, 201);
    if (method === "POST" && path.endsWith("/drafts")) {
      generation++; configurationVersion++;
      const revision = { ref: `revision${generation}`, revision: generation, state: "DRAFT", content: body.content, digest: h(body.content), sourceAvailable: true, ...(publishedRevision ? { parentRevisionRef: publishedRevision.ref } : {}) }; revisions.push(revision);
      return response({ configuration: configuration(), revision }, 201);
    }
    if (method === "POST" && /\/(validation|publication)$/.test(path)) {
      const revision = revisions.at(-1); revision.state = path.endsWith("validation") ? "VALID" : "PUBLISHED"; configurationVersion++; if (path.endsWith("publication")) publishedRevision = revision;
      return response({ configuration: configuration(), revision });
    }
    if (method === "GET" && url.pathname === "/api/v1/managed-configurations") return response({ items: [configuration()] });
    if (method === "GET" && url.pathname.endsWith("/role-image-recipes")) return response({ items: [{ ...artifactDetail(generation).recipe, name: fixtureName(), roleDefinitionRef: "role" }] });
    if (method === "GET" && url.pathname.endsWith("/role-image-recipes/recipe")) return response(artifactDetail(generation));
    if (method === "POST" && path.endsWith("/promotions")) return response({ ref: `promotion${generation}`, recipeRef: "recipe", imageArtifactRef: `artifact${generation}`, state: "QUEUED" }, 202);
    if (method === "POST" && path.endsWith("/runtime-environments")) return response({ ref: "environment" }, 201);
    if (method === "PUT" && path.endsWith("/runtime-environment-binding")) { bound = true; activeReference = artifactDetail(generation).activeArtifact.promotedReference; activeArtifactRef = `artifact${generation}`; return response({ environment: { currentVersion: { image: { reference: activeReference } } } }); }
    if (method === "GET" && url.pathname.endsWith("/revisions")) return response({ configuration: configuration(), items: revisions });
    const impact = { ref: `plan${generation}`, digest, configurationRef: "configuration", revisionRef: `revision${generation}`, artifactRef: `artifact${generation}`, total: 2 };
    if (method === "POST" && path.endsWith("/impact-plans")) return response(impact, 201);
    if (method === "GET" && url.pathname.includes("/role-image-impact-plans/")) return response({ items: ["environment-item", "agent-item"].map((ref) => ({ ref, environmentRef: "environment", projectRef: "project", outcome: "APPLIED" })) });
    if (method === "POST" && path.endsWith("/consumer-bindings")) { assert.equal(body.selectedItemRefs.length, 2); activeArtifactRef = `artifact${generation}`; return response({ plan: { ...impact, state: "APPLIED" } }); }
    if (method === "GET" && path.endsWith("/runtime-configuration")) { assert.equal(bound, true); return response({ environment: { ref: "environment", currentVersion: { ref: "envversion", image: { reference: activeReference, artifactRef: activeArtifactRef } } }, environmentBinding: { ref: "binding", version: 1, digest, agentRef: "agent", environmentRef: "environment", versionRef: "envversion" } }); }
    throw new Error(`unhandled test route ${method} ${path}`);
  };
  return { request, revisions, get generation() { return generation; }, get mutations() { return mutations; } };
}
