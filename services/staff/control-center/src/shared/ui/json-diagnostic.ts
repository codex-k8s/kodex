import { jsonLanguage } from "@codemirror/lang-json";

export function jsonSyntaxIssue(
  source: string,
): { from: number; to: number; line: number; column: number } | undefined {
  try {
    JSON.parse(source);
    return undefined;
  } catch {
    /* Позиция берётся из parser, текст исключения не используется. */
  }
  let position: number | undefined;
  jsonLanguage.parser.parse(source).iterate({
    enter(node) {
      if (position !== undefined) return false;
      if (node.type.isError) position = node.from;
    },
  });
  const from = Math.min(position ?? 0, source.length);
  const prefix = source.slice(0, from);
  return {
    from,
    to: Math.min(from + 1, source.length),
    line: prefix.split("\n").length,
    column: from - prefix.lastIndexOf("\n"),
  };
}
