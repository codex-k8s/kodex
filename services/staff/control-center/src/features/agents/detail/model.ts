import type {
  RuntimeSelection,
  TemplateVariable,
} from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityPickerItem } from "@/shared/ui/async-entity-picker";

export type AgentDetailTab =
  | "profile"
  | "instructions"
  | "runtime"
  | "environment"
  | "access";

export type ApplyBoundary = "next-run" | "next-turn" | "published";

export interface AgentProfileDraft {
  name: string;
  purpose: string;
  roleDescription: string;
}

export type AgentBackendFeatureCode = "avatar_asset";

export type AgentBackendFeatureAvailability =
  | { state: "AVAILABLE" }
  | {
      state: "UNAVAILABLE";
      code: AgentBackendFeatureCode;
      reason: string;
    };

export interface CodeToken {
  text: string;
  tone:
    | "plain"
    | "comment"
    | "keyword"
    | "section"
    | "string"
    | "number"
    | "variable"
    | "strong";
}

export interface TemplateVariablePickerItem extends AsyncEntityPickerItem {
  variable: TemplateVariable;
  scope: string;
}

export interface TextInsertionResult {
  value: string;
  selectionStart: number;
  selectionEnd: number;
}

export function agentInitials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "AI";
  const first = parts[0]?.[0] ?? "";
  const second = (parts.length > 1 ? parts.at(-1)?.[0] : parts[0]?.[1]) ?? "";
  return (first + second).toLocaleUpperCase().slice(0, 2);
}

export function readyRuntimes(
  runtimes: readonly RuntimeSelection[],
): RuntimeSelection[] {
  return [...runtimes]
    .filter((runtime) => runtime.ready)
    .sort(
      (left, right) =>
        left.provider.localeCompare(right.provider) ||
        left.model.localeCompare(right.model) ||
        left.name.localeCompare(right.name),
    );
}

export function runtimeProviders(
  runtimes: readonly RuntimeSelection[],
): string[] {
  return [
    ...new Set(readyRuntimes(runtimes).map((runtime) => runtime.provider)),
  ];
}

export function runtimeModels(
  runtimes: readonly RuntimeSelection[],
  provider: string,
): string[] {
  return [
    ...new Set(
      readyRuntimes(runtimes)
        .filter((runtime) => runtime.provider === provider)
        .map((runtime) => runtime.model),
    ),
  ];
}

export function runtimesForSelection(
  runtimes: readonly RuntimeSelection[],
  provider: string,
  model: string,
): RuntimeSelection[] {
  return readyRuntimes(runtimes).filter(
    (runtime) => runtime.provider === provider && runtime.model === model,
  );
}

export function runtimeRefForSelection(
  runtimes: readonly RuntimeSelection[],
  provider: string,
  model?: string,
): string | undefined {
  const providerRuntimes = readyRuntimes(runtimes).filter(
    (runtime) => runtime.provider === provider,
  );
  if (!model) return providerRuntimes[0]?.ref;
  return providerRuntimes.find((runtime) => runtime.model === model)?.ref;
}

export function sameProfileDraft(
  draft: AgentProfileDraft,
  current: AgentProfileDraft,
): boolean {
  return (
    draft.name === current.name &&
    draft.purpose === current.purpose &&
    draft.roleDescription === current.roleDescription
  );
}

export function toTemplateVariablePickerItem(
  variable: TemplateVariable,
): TemplateVariablePickerItem {
  return {
    id: variable.name,
    label: variable.name,
    description: variable.description,
    scope: variable.source,
    variable,
  };
}

export function templateVariableInsertion(name: string): string {
  return `{{${name}}}`;
}

export function insertTextAtSelection(
  value: string,
  insertion: string,
  selectionStart: number,
  selectionEnd: number,
): TextInsertionResult {
  const start = Math.max(0, Math.min(selectionStart, value.length));
  const end = Math.max(start, Math.min(selectionEnd, value.length));
  const nextCursor = start + insertion.length;
  return {
    value: `${value.slice(0, start)}${insertion}${value.slice(end)}`,
    selectionStart: nextCursor,
    selectionEnd: nextCursor,
  };
}

export function extractTemplateVariables(content: string): string[] {
  return [
    ...new Set(
      [...content.matchAll(/\{\{\s*([A-Za-z][A-Za-z0-9_.-]*)\s*}}/g)].flatMap(
        (match) => (match[1] ? [`{{${match[1]}}}`] : []),
      ),
    ),
  ].sort();
}

function inlineTokens(value: string): CodeToken[] {
  const pattern =
    /(\{\{\s*[A-Za-z][A-Za-z0-9_.-]*\s*}}|`[^`]*`|\*\*[^*]+\*\*|"[^"\n]*"|\b(?:true|false)\b|\b\d+(?:\.\d+)?\b)/g;
  const tokens: CodeToken[] = [];
  let cursor = 0;
  for (const match of value.matchAll(pattern)) {
    const index = match.index;
    if (index > cursor)
      tokens.push({ text: value.slice(cursor, index), tone: "plain" });
    const text = match[0];
    const tone: CodeToken["tone"] = text.startsWith("{{")
      ? "variable"
      : text.startsWith("`")
        ? "keyword"
        : text.startsWith("**")
          ? "strong"
          : text.startsWith('"')
            ? "string"
            : text === "true" || text === "false"
              ? "keyword"
              : "number";
    tokens.push({ text, tone });
    cursor = index + text.length;
  }
  if (cursor < value.length)
    tokens.push({ text: value.slice(cursor), tone: "plain" });
  return tokens.length > 0 ? tokens : [{ text: value || " ", tone: "plain" }];
}

export function tokenizeCodeLine(
  line: string,
  language: "markdown" | "toml",
): CodeToken[] {
  if (language === "toml") {
    if (/^\s*#/.test(line)) return [{ text: line || " ", tone: "comment" }];
    if (/^\s*\[[^\]]+]\s*$/.test(line))
      return [{ text: line, tone: "section" }];
    const assignment = /^(\s*[A-Za-z0-9_.-]+)(\s*=\s*)(.*)$/.exec(line);
    if (assignment)
      return [
        { text: assignment[1] ?? "", tone: "keyword" },
        { text: assignment[2] ?? "", tone: "plain" },
        ...inlineTokens(assignment[3] ?? ""),
      ];
    return inlineTokens(line);
  }

  const prefix = /^(\s*(?:#{1,6}|[-*>]|\d+[.)]))(\s+)(.*)$/.exec(line);
  if (!prefix) return inlineTokens(line);
  return [
    { text: prefix[1] ?? "", tone: "section" },
    { text: prefix[2] ?? "", tone: "plain" },
    ...inlineTokens(prefix[3] ?? ""),
  ];
}
