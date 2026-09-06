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
  context?: CodeEditorCompletionContext,
) => Promise<readonly CodeEditorCompletionItem[]>;
export interface CodeEditorCompletionContext {
  content: string;
  cursor: number;
}
export interface CodeEditorHover {
  from: number;
  to: number;
  text: string;
}
export type CodeEditorHoverProvider = (
  content: string,
  position: number,
) => CodeEditorHover | undefined;
export interface CodeEditorPositionedDiagnostic {
  line: number;
  column: number;
  message: string;
}
export function positionedCodeEditorDiagnostics(
  messages: readonly CodeEditorPositionedDiagnostic[],
  content: string,
): Diagnostic[] {
  const normalized = content.replace(/\r\n?/g, "\n");
  const lines = normalized.split("\n");
  return messages.flatMap((message) => {
    if (
      !Number.isSafeInteger(message.line) ||
      !Number.isSafeInteger(message.column) ||
      message.line < 1 ||
      message.column < 1
    )
      return [];
    const line = lines[message.line - 1];
    if (line === undefined) return [];
    const bytes = new TextEncoder().encode(line);
    if (message.column - 1 > bytes.length) return [];
    let prefix: string;
    try {
      prefix = new TextDecoder("utf-8", { fatal: true }).decode(
        bytes.slice(0, message.column - 1),
      );
    } catch {
      return [];
    }
    const from =
      lines
        .slice(0, message.line - 1)
        .reduce((offset, item) => offset + item.length + 1, 0) + prefix.length;
    const size = (normalized.codePointAt(from) ?? 0) > 0xffff ? 2 : 1;
    return [
      {
        from,
        to: Math.min(from + size, normalized.length),
        message: message.message,
        severity: "error" as const,
      },
    ];
  });
}
export function tomlCompletionQuery(
  content: string,
  cursor: number,
  explicit: boolean,
): TemplateCompletionQuery | undefined {
  const before = content.slice(0, cursor);
  const line = before.slice(before.lastIndexOf("\n") + 1);
  const value = /^\s*[^=]+\s*=\s*("[^"\n]*|[a-z_-]*)$/.exec(line);
  if (value) {
    const after = content.slice(cursor).split("\n")[0] ?? "";
    const quoted = value[1]?.startsWith('"');
    const suffix = quoted
      ? after.indexOf('"') >= 0
        ? after.indexOf('"') + 1
        : 0
      : (/^[a-z_-]*/.exec(after)?.[0].length ?? 0);
    return {
      from: before.length - (value[1]?.length ?? 0),
      to: cursor + suffix,
      query: value[1] ?? "",
    };
  }
  const key = /^\s*([A-Za-z_".][A-Za-z0-9_".-]*)?$/.exec(line);
  return key && (explicit || key[1])
    ? { from: before.length - (key[1]?.length ?? 0), query: key[1] ?? "" }
    : undefined;
}

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
  to?: number;
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
