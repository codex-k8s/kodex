import type { WorkflowInputField } from "@/shared/api/generated/openapi/types.gen";

export interface PreparedAssistantWorkflowInput {
  value: Record<string, unknown>;
  problems: Record<string, boolean>;
}

function validCalendarDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.toISOString().slice(0, 10) === value
  );
}

export function prepareAssistantWorkflowInput(
  fields: readonly WorkflowInputField[],
  rawInput: Readonly<Record<string, string>>,
): PreparedAssistantWorkflowInput {
  const known = new Set(fields.map((field) => field.key));
  const problems: Record<string, boolean> = {};
  const value: Record<string, unknown> = {};
  if (Object.keys(rawInput).some((key) => !known.has(key)))
    problems.unknown = true;
  for (const field of fields) {
    const raw = rawInput[field.key]?.trim() ?? "";
    if (field.valueType === "BOOLEAN") {
      if (raw && raw !== "true" && raw !== "false") problems[field.key] = true;
      else value[field.key] = raw === "true";
      continue;
    }
    if (!raw) {
      if (field.required) problems[field.key] = true;
      continue;
    }
    if (field.valueType === "NUMBER") {
      const number = Number(raw);
      if (!Number.isFinite(number)) problems[field.key] = true;
      else value[field.key] = number;
    } else if (
      (field.valueType === "SELECT" && !field.options.includes(raw)) ||
      (field.valueType === "TEXT" && raw.length > 4000) ||
      (field.valueType === "LONG_TEXT" && raw.length > 32768) ||
      (field.valueType === "DATE" && !validCalendarDate(raw))
    )
      problems[field.key] = true;
    else value[field.key] = raw;
  }
  return { value, problems };
}
