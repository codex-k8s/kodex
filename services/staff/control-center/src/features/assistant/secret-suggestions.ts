import type { RuntimeSecretDraftSuggestion } from "@/features/runtime-secrets/model";

export type AssistantSecretSuggestion = RuntimeSecretDraftSuggestion;

export function parseAssistantSecretSuggestions(
  raw: unknown,
): AssistantSecretSuggestion[] | undefined {
  if (raw === undefined) return [];
  if (!Array.isArray(raw) || raw.length > 8) return undefined;
  const result: AssistantSecretSuggestion[] = [];
  const names = new Set<string>();
  for (const entry of raw) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry))
      return undefined;
    const item = entry as Record<string, unknown>;
    if (
      Object.keys(item).some(
        (key) =>
          !["name", "description", "valueType", "sourceHelp"].includes(key),
      ) ||
      typeof item.name !== "string" ||
      !item.name.trim() ||
      item.name !== item.name.trim() ||
      item.name.length > 120 ||
      (item.description !== undefined &&
        (typeof item.description !== "string" ||
          item.description.length > 1000)) ||
      typeof item.valueType !== "string" ||
      !["STRING", "JSON", "BINARY"].includes(item.valueType) ||
      typeof item.sourceHelp !== "string" ||
      !item.sourceHelp.trim() ||
      item.sourceHelp.length > 1000 ||
      names.has(item.name)
    )
      return undefined;
    names.add(item.name);
    result.push({
      name: item.name,
      description: typeof item.description === "string" ? item.description : "",
      valueType: item.valueType as AssistantSecretSuggestion["valueType"],
      sourceHelp: item.sourceHelp,
    });
  }
  return result;
}
