import type { InstructionCommand } from "@/shared/api/generated/openapi/types.gen";

export function instructionCommandInput(
  action: InstructionCommand["action"],
  publishedInstructionRef?: string,
): InstructionCommand {
  if (action === "PUBLISH")
    throw new Error("Instruction publication requires an impact plan");
  if (action === "ROLLBACK") {
    if (!publishedInstructionRef)
      throw new Error("Published instruction reference is required");
    return { action, publishedInstructionRef };
  }
  return { action };
}
