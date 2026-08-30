import type { StreamParser } from "@codemirror/language";
import type { Diagnostic } from "@codemirror/lint";

export interface CodeEditorCompletionItem {
  label: string;
  apply?: string;
  detail?: string;
  type?: string;
}

export type CodeEditorCompletionProvider = (
  query: string,
  signal: AbortSignal,
) => Promise<readonly CodeEditorCompletionItem[]>;

export interface CodeEditorAccessibilityInput {
  label: string;
  readonly: boolean;
  invalid: boolean;
  describedBy: string;
  errorMessageId: string;
}

export function codeEditorContentAttributes(
  input: CodeEditorAccessibilityInput,
): Record<string, string> {
  return {
    role: "textbox",
    "aria-label": input.label,
    "aria-multiline": "true",
    "aria-readonly": String(input.readonly),
    "aria-invalid": String(input.invalid),
    "aria-describedby": input.describedBy,
    ...(input.invalid ? { "aria-errormessage": input.errorMessageId } : {}),
    spellcheck: "false",
  };
}

interface MarkdownState {
  fenced: boolean;
}

export const markdownStreamParser: StreamParser<MarkdownState> = {
  startState: () => ({ fenced: false }),
  copyState: (state) => ({ ...state }),
  token(stream, state) {
    if (stream.sol()) {
      if (stream.match(/^\s{0,3}```/)) {
        state.fenced = !state.fenced;
        stream.skipToEnd();
        return "keyword";
      }
      if (state.fenced) {
        stream.skipToEnd();
        return "monospace";
      }
      if (stream.match(/^\s{0,3}#{1,6}(?=\s)/)) return "heading";
      if (stream.match(/^\s*(?:[-+*]|\d+[.)])(?=\s)/)) return "list";
      if (stream.match(/^\s*>\s?/)) return "quote";
    }

    if (stream.match(/^\{\{\s*(?:range\s+)?\.?[A-Za-z][A-Za-z0-9_.-]*\s*}}/))
      return "variableName";
    if (stream.match(/^`[^`]*`/)) return "monospace";
    if (stream.match(/^\*\*[^*]+\*\*/)) return "strong";
    if (stream.match(/^\[[^\]]+]\([^\s)]+\)/)) return "link";
    stream.next();
    return null;
  },
};

export interface TemplateCompletionQuery {
  from: number;
  query: string;
}

export function templateCompletionQuery(
  content: string,
  cursor: number,
  explicit: boolean,
): TemplateCompletionQuery | undefined {
  const before = content.slice(
    0,
    Math.max(0, Math.min(cursor, content.length)),
  );
  const match = /\{\{\s*(?:range\s+)?\.?([A-Za-z][A-Za-z0-9_.-]*)?$/.exec(
    before,
  );
  if (match)
    return {
      from: before.length - match[0].length,
      query: match[1] ?? "",
    };
  return explicit ? { from: before.length, query: "" } : undefined;
}

export function codeEditorDiagnostics(
  messages: readonly string[],
  documentLength: number,
): Diagnostic[] {
  const to = Math.min(Math.max(documentLength, 0), 1);
  return messages.map((message) => ({
    from: 0,
    to,
    severity: "error",
    message,
  }));
}

export function codeMirrorPhrases(locale: string): Record<string, string> {
  if (!locale.toLocaleLowerCase().startsWith("ru")) return {};
  return {
    Find: "Найти",
    Replace: "Заменить",
    next: "следующее",
    previous: "предыдущее",
    all: "все",
    "match case": "учитывать регистр",
    regexp: "регулярное выражение",
    "by word": "слово целиком",
    replace: "заменить",
    "replace all": "заменить всё",
    close: "закрыть",
    "current match": "текущее совпадение",
    "on line": "в строке",
    "Go to line": "Перейти к строке",
    go: "перейти",
    Completions: "Варианты дополнения",
    Diagnostics: "Диагностика",
    "No diagnostics": "Диагностика отсутствует",
  };
}
