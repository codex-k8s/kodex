import { parse, stringify } from "smol-toml";
import type { ConfigOverlaySchema } from "@/shared/api/generated/openapi/types.gen";
import type {
  CodeEditorCompletionContext,
  CodeEditorCompletionItem,
  CodeEditorHover,
} from "./code-editor";

function parsedOverlay(content: string, schema: ConfigOverlaySchema) {
  if (
    new TextEncoder().encode(content).length > schema.maximumBytes ||
    content.includes("\0") ||
    new TextDecoder().decode(new TextEncoder().encode(content)) !== content
  )
    throw new Error("Overlay size or encoding is invalid");
  try {
    return parse(content);
  } catch {
    throw new Error("Overlay TOML syntax is invalid");
  }
}
export function overlayEffort(
  content: string,
  schema: ConfigOverlaySchema,
): string | undefined {
  try {
    const value = parsedOverlay(content, schema).model_reasoning_effort;
    return value === undefined
      ? ""
      : typeof value === "string"
        ? value
        : undefined;
  } catch {
    return undefined;
  }
}
export function changeOverlayEffort(
  content: string,
  schema: ConfigOverlaySchema,
  effort: string,
): string {
  const field = schema.fields.find(
    (item) => item.key === "model_reasoning_effort",
  );
  if (!field || (effort && !field.allowedValues.includes(effort)))
    throw new Error("Overlay reasoning effort is unavailable");
  const parsed = parsedOverlay(content, schema);
  function check(value: unknown, prefix: string): void {
    if (!value || typeof value !== "object" || Array.isArray(value))
      throw new Error("Overlay field is invalid");
    for (const [key, item] of Object.entries(value)) {
      const path = prefix + key;
      if (path === field?.key) continue;
      const descriptor = schema.fields.find((entry) => entry.key === path);
      if (descriptor) {
        if (
          typeof item !== descriptor.valueType ||
          !descriptor.allowedValues.includes(String(item))
        )
          throw new Error("Overlay field is invalid");
      } else if (
        schema.fields.some((entry) => entry.key.startsWith(`${path}.`))
      )
        check(item, `${path}.`);
      else throw new Error("Overlay field is forbidden");
    }
  }
  check(parsed, "");
  if (effort) parsed.model_reasoning_effort = effort;
  else Reflect.deleteProperty(parsed, "model_reasoning_effort");
  return stringify(parsed);
}
function markerPath(content: string): string | undefined {
  const marker = "__kodex_completion_probe__";
  try {
    const parsed = parse(`${content}\n${marker} = true\n`);
    const paths: string[] = [];
    function visit(value: unknown, path: string): void {
      if (!value || typeof value !== "object" || Array.isArray(value)) return;
      for (const [key, item] of Object.entries(value)) {
        if (key === marker && item === true) paths.push(path);
        else visit(item, `${path}${key}.`);
      }
    }
    visit(parsed, "");
    return paths.length === 1 ? paths[0] : undefined;
  } catch {
    return undefined;
  }
}
function fieldPath(key: string): string | undefined {
  try {
    let value: unknown = parse(`${key} = true`);
    const path: string[] = [];
    while (value !== true) {
      if (!value || typeof value !== "object" || Array.isArray(value))
        return undefined;
      const entries = Object.entries(value);
      if (entries.length !== 1 || !entries[0]) return undefined;
      path.push(entries[0][0]);
      value = entries[0][1];
    }
    return path.join(".");
  } catch {
    return undefined;
  }
}
export function overlayCompletions(
  schema: ConfigOverlaySchema,
  query: string,
  context?: CodeEditorCompletionContext,
): CodeEditorCompletionItem[] {
  if (!context) return [];
  const start = context.content.lastIndexOf("\n", context.cursor - 1) + 1;
  const scope = markerPath(context.content.slice(0, start));
  if (scope === undefined) return [];
  const line = context.content.slice(start, context.cursor);
  const separator = line.indexOf("=");
  if (separator >= 0) {
    const key = fieldPath(line.slice(0, separator).trim());
    const field = schema.fields.find(
      (item) => item.key === scope + (key ?? ""),
    );
    return (
      field?.allowedValues.map((value) => ({
        label: value,
        apply: field.valueType === "string" ? JSON.stringify(value) : value,
        detail: field.description,
        type: "constant",
      })) ?? []
    );
  }
  return schema.fields
    .filter(
      (field) =>
        field.key.startsWith(scope) &&
        field.key.slice(scope.length).startsWith(query.replace(/^"/, "")),
    )
    .map((field) => ({
      label: field.key.slice(scope.length),
      apply: field.completion.startsWith(scope)
        ? field.completion.slice(scope.length)
        : field.completion,
      detail: field.description,
      type: "property",
    }));
}
export function overlayHover(
  schema: ConfigOverlaySchema,
  content: string,
  position: number,
): CodeEditorHover | undefined {
  const start = content.lastIndexOf("\n", position - 1) + 1;
  const end = content.indexOf("\n", position);
  const line = content.slice(start, end < 0 ? undefined : end);
  const separator = line.indexOf("=");
  if (separator < 0 || position > start + separator) return undefined;
  const key = fieldPath(line.slice(0, separator).trim());
  const scope = markerPath(content.slice(0, start));
  if (!key || scope === undefined) return undefined;
  const field = schema.fields.find((item) => item.key === scope + key);
  return field
    ? {
        from: start,
        to: start + separator,
        text: `${field.description}\n${field.hover}\n${field.allowedValues.join(" · ")}`,
      }
    : undefined;
}
