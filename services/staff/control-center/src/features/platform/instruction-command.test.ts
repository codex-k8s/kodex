import { describe, expect, it } from "vitest";

import { instructionCommandInput } from "@/features/platform/instruction-command";

describe("instruction command", () => {
  it("связывает rollback с выбранной опубликованной версией", () => {
    expect(instructionCommandInput("ROLLBACK", "ins_previous")).toEqual({
      action: "ROLLBACK",
      publishedInstructionRef: "ins_previous",
    });
    expect(() => instructionCommandInput("ROLLBACK")).toThrow(
      "Published instruction reference is required",
    );
  });

  it("не добавляет revision ref в validate и не публикует без плана", () => {
    expect(instructionCommandInput("VALIDATE", "ins_ignored")).toEqual({
      action: "VALIDATE",
    });
    expect(() => instructionCommandInput("PUBLISH")).toThrow(
      "Instruction publication requires an impact plan",
    );
  });
});
