import { expect, it } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import Component from "./RuntimeEnvironmentToolsEditor.vue";

it("сворачивает заполненные метаданные, но показывает обязательное незаполненное описание", async () => {
  const tools = [
    {
      name: "Git",
      command: "git",
      description: "Проверка дерева",
      usageHint: "status",
    },
    { name: "Node", command: "node", description: "", usageHint: "--version" },
  ];
  const before = structuredClone(tools);
  const state = await captureSetupState(Component, undefined, {
    tools,
    catalog: [
      { name: "git", version: "2" },
      { name: "node", version: "24" },
    ],
    disabled: false,
    imageSelected: true,
  });
  const expanded = state.expandedTools as Map<string, boolean>;
  expect(expanded.get("git")).toBe(false);
  expect(expanded.get("node")).toBe(true);
  expect(tools).toEqual(before);
});
