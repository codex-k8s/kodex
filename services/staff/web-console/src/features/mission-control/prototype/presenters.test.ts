import test from "node:test";
import assert from "node:assert/strict";

import { missionControlPrototypeScenarios } from "./fixtures.ts";
import {
  buildInitiativeViews,
  buildNodeViews,
  buildRelationViews,
  buildViewportForNodes,
  missionControlPrototypeCardWidth,
} from "./presenters.ts";
import { missionControlPrototypeSource } from "./source.ts";

test("buildNodeViews dims unrelated initiatives and preserves search matches", () => {
  const scenario = missionControlPrototypeScenarios[0];
  const nodeViews = buildNodeViews(scenario, {
    search: "owner",
    focusedInitiativeId: "initiative-s18",
    selectedNodeId: "issue-581",
  });

  const issueNode = nodeViews.find((node) => node.nodeId === "issue-581");
  const qualityNode = nodeViews.find((node) => node.nodeId === "issue-524");

  assert.ok(issueNode);
  assert.equal(issueNode?.dimmed, false);
  assert.equal(issueNode?.matchesSearch, true);

  assert.ok(qualityNode);
  assert.equal(qualityNode?.dimmed, true);
});

test("buildRelationViews keeps selected-node edges highlighted", () => {
  const scenario = missionControlPrototypeScenarios[0];
  const nodeViews = buildNodeViews(scenario, {
    search: "",
    focusedInitiativeId: null,
    selectedNodeId: "pr-s18-owner-demo",
  });
  const relationViews = buildRelationViews(scenario, nodeViews, "pr-s18-owner-demo");

  const highlightedRelation = relationViews.find((relation) => relation.relationId === "rel-pr-blocks-563");
  assert.ok(highlightedRelation);
  assert.equal(highlightedRelation?.highlighted, true);
});

test("buildViewportForNodes centers a focused initiative", () => {
  const scenario = missionControlPrototypeScenarios[0];
  const nodes = scenario.nodes.filter((node) => node.initiativeId === "initiative-s18");
  const viewport = buildViewportForNodes(nodes, scenario.defaultViewport);

  assert.ok(viewport.zoomLevel <= 1.15);
  assert.ok(viewport.canvasWidth > missionControlPrototypeCardWidth);
  assert.ok(viewport.panX >= 0);
});

test("source generates deterministic workflow preview with source refs", async () => {
  const preview = await missionControlPrototypeSource.generateWorkflowPreview({
    scenarioId: "owner-walkthrough",
    nodeId: "issue-581",
    presetId: "preset-owner-demo",
    draft: {
      stageSequenceVariant: "revise_loop",
      autoReviewPolicy: "required",
      followUpPolicy: "carry_to_next_stage",
      safeActionProfile: "github_links_only",
    },
  });

  assert.equal(preview.presetId, "preset-owner-demo");
  assert.match(preview.generatedBlockMarkdown, /workflow-policy:/);
  assert.ok(preview.sourceRefs.length >= 2);
  assert.ok(preview.changeExplanations.some((item) => item.includes("stage_sequence_variant")));
});

test("buildInitiativeViews returns deterministic cluster bounds", () => {
  const scenario = missionControlPrototypeScenarios[0];
  const initiativeViews = buildInitiativeViews(scenario, "initiative-s18");
  const s18View = initiativeViews.find((initiative) => initiative.initiativeId === "initiative-s18");

  assert.ok(s18View);
  assert.equal(s18View?.focused, true);
  assert.ok((s18View?.bounds.width ?? 0) > 0);
});
