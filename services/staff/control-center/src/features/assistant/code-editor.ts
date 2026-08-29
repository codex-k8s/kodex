export interface AssistantCodeToken {
  text: string;
  tone: "plain" | "key" | "string" | "number" | "keyword";
}

const jsonTokenPattern =
  /("(?:\\.|[^"\\])*"\s*:)|("(?:\\.|[^"\\])*")|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false|null)\b/g;

export function tokenizeAssistantCodeLine(
  line: string,
  language: "json" | "text",
): AssistantCodeToken[] {
  if (language === "text" || line === "")
    return [{ text: line, tone: "plain" }];
  const result: AssistantCodeToken[] = [];
  let offset = 0;
  for (const match of line.matchAll(jsonTokenPattern)) {
    const index = match.index;
    if (index > offset)
      result.push({ text: line.slice(offset, index), tone: "plain" });
    const text = match[0];
    result.push({
      text,
      tone: match[1]
        ? "key"
        : match[2]
          ? "string"
          : match[3]
            ? "number"
            : "keyword",
    });
    offset = index + text.length;
  }
  if (offset < line.length)
    result.push({ text: line.slice(offset), tone: "plain" });
  return result;
}

export function validateAssistantObjectJSON(value: string): boolean {
  try {
    const parsed: unknown = JSON.parse(value);
    return (
      typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)
    );
  } catch {
    return false;
  }
}
