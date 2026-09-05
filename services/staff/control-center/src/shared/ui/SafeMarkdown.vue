<script setup lang="ts">
import { useServerMessage } from "@/shared/ui/server-message";
import { computed } from "vue";

import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";

type InlineToken = {
  type: "text" | "code" | "strong" | "emphasis" | "link";
  text: string;
  href?: string;
};

type MarkdownBlock =
  | { type: "paragraph" | "quote"; tokens: InlineToken[] }
  | { type: "heading"; level: number; tokens: InlineToken[] }
  | { type: "code"; text: string }
  | { type: "unordered-list" | "ordered-list"; items: InlineToken[][] }
  | { type: "table"; header: InlineToken[][]; rows: InlineToken[][][] };

const props = defineProps<{ content: string }>();
const opaqueRefPattern =
  /\b(?:agt|art|bld|cap|cnv|con|edg|evt|gat|inc|int|job|mbr|msg|nod|pln|prj|rev|rol|rti|run|sch|ses|trn|usr|wfl)_[A-Za-z0-9_-]{8,}\b/g;

function safeText(value: string): string {
  return value.replace(opaqueRefPattern, "—");
}

function safeHref(raw: string): string | undefined {
  const href = raw.trim();
  if (href.startsWith("/") || href.startsWith("#")) return href;
  try {
    const parsed = new URL(href);
    return ["https:", "http:", "mailto:"].includes(parsed.protocol)
      ? parsed.toString()
      : undefined;
  } catch {
    return undefined;
  }
}

function parseInline(source: string): InlineToken[] {
  const tokens: InlineToken[] = [];
  const pattern =
    /(!?)\[([^\]]+)]\(([^)\s]+)(?:\s+"[^"]*")?\)|`([^`]+)`|\*\*([^*]+)\*\*|__([^_]+)__|(?<!\*)\*([^*]+)\*(?!\*)|(?<!_)_([^_]+)_(?!_)/g;
  let cursor = 0;
  for (const match of source.matchAll(pattern)) {
    const index = match.index;
    if (index > cursor)
      tokens.push({
        type: "text",
        text: safeText(source.slice(cursor, index)),
      });

    if (match[2] !== undefined) {
      if (match[1] === "!") {
        // Images are deliberately reduced to their alternative text.
        tokens.push({ type: "text", text: safeText(match[2]) });
      } else {
        const href = safeHref(match[3] ?? "");
        tokens.push(
          href
            ? { type: "link", text: safeText(match[2]), href }
            : { type: "text", text: safeText(match[2]) },
        );
      }
    } else if (match[4] !== undefined) {
      tokens.push({ type: "code", text: safeText(match[4]) });
    } else if (match[5] !== undefined || match[6] !== undefined) {
      tokens.push({
        type: "strong",
        text: safeText(match[5] ?? match[6] ?? ""),
      });
    } else {
      tokens.push({
        type: "emphasis",
        text: safeText(match[7] ?? match[8] ?? ""),
      });
    }
    cursor = index + match[0].length;
  }
  if (cursor < source.length)
    tokens.push({ type: "text", text: safeText(source.slice(cursor)) });
  return tokens.length ? tokens : [{ type: "text", text: safeText(source) }];
}

function tableCells(line: string): string[] {
  return line
    .trim()
    .replace(/^\||\|$/g, "")
    .split("|")
    .map((cell) => cell.trim());
}

function isTableDivider(line: string): boolean {
  const cells = tableCells(line);
  return cells.length > 0 && cells.every((cell) => /^:?-{3,}:?$/.test(cell));
}

function parseBlocks(source: string): MarkdownBlock[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: MarkdownBlock[] = [];
  for (let index = 0; index < lines.length; ) {
    const line = lines[index] ?? "";
    if (!line.trim()) {
      index += 1;
      continue;
    }
    if (/^```/.test(line.trim())) {
      const code: string[] = [];
      index += 1;
      while (
        index < lines.length &&
        !/^```/.test((lines[index] ?? "").trim())
      ) {
        code.push(lines[index] ?? "");
        index += 1;
      }
      if (index < lines.length) index += 1;
      blocks.push({ type: "code", text: safeText(code.join("\n")) });
      continue;
    }
    const heading = /^(#{1,4})\s+(.+)$/.exec(line);
    if (heading) {
      blocks.push({
        type: "heading",
        level: heading[1]?.length ?? 2,
        tokens: parseInline(heading[2] ?? ""),
      });
      index += 1;
      continue;
    }
    if (
      line.includes("|") &&
      index + 1 < lines.length &&
      isTableDivider(lines[index + 1] ?? "")
    ) {
      const header = tableCells(line).map(parseInline);
      const rows: InlineToken[][][] = [];
      index += 2;
      while (index < lines.length && (lines[index] ?? "").includes("|")) {
        rows.push(tableCells(lines[index] ?? "").map(parseInline));
        index += 1;
      }
      blocks.push({ type: "table", header, rows });
      continue;
    }
    if (/^\s*[-*]\s+/.test(line)) {
      const items: InlineToken[][] = [];
      while (index < lines.length && /^\s*[-*]\s+/.test(lines[index] ?? "")) {
        items.push(
          parseInline((lines[index] ?? "").replace(/^\s*[-*]\s+/, "")),
        );
        index += 1;
      }
      blocks.push({ type: "unordered-list", items });
      continue;
    }
    if (/^\s*\d+[.)]\s+/.test(line)) {
      const items: InlineToken[][] = [];
      while (
        index < lines.length &&
        /^\s*\d+[.)]\s+/.test(lines[index] ?? "")
      ) {
        items.push(
          parseInline((lines[index] ?? "").replace(/^\s*\d+[.)]\s+/, "")),
        );
        index += 1;
      }
      blocks.push({ type: "ordered-list", items });
      continue;
    }
    if (/^\s*>\s?/.test(line)) {
      const quote: string[] = [];
      while (index < lines.length && /^\s*>\s?/.test(lines[index] ?? "")) {
        quote.push((lines[index] ?? "").replace(/^\s*>\s?/, ""));
        index += 1;
      }
      blocks.push({ type: "quote", tokens: parseInline(quote.join(" ")) });
      continue;
    }

    const paragraph = [line.trim()];
    index += 1;
    while (index < lines.length && (lines[index] ?? "").trim()) {
      const next = lines[index] ?? "";
      if (
        /^```|^#{1,4}\s|^\s*[-*]\s+|^\s*\d+[.)]\s+|^\s*>\s?/.test(next) ||
        (next.includes("|") && isTableDivider(lines[index + 1] ?? ""))
      )
        break;
      paragraph.push(next.trim());
      index += 1;
    }
    blocks.push({
      type: "paragraph",
      tokens: parseInline(paragraph.join(" ")),
    });
  }
  return blocks;
}

function parseStructuredResult(source: string): object | undefined {
  let candidate = source.trim();
  const fenced = /^```json\s*([\s\S]*?)\s*```$/i.exec(candidate);
  if (fenced) candidate = fenced[1] ?? "";
  if (!candidate.startsWith("{") && !candidate.startsWith("["))
    return undefined;
  try {
    const parsed: unknown = JSON.parse(candidate);
    return typeof parsed === "object" && parsed !== null ? parsed : undefined;
  } catch {
    return undefined;
  }
}

const structuredResult = computed(() => parseStructuredResult(props.content));
const blocks = computed(() =>
  structuredResult.value === undefined
    ? parseBlocks(serverMessage(props.content))
    : [],
);
const serverMessage = useServerMessage();
</script>

<template>
  <div class="safe-markdown">
    <SafeStructuredData
      v-if="structuredResult !== undefined"
      :value="structuredResult"
    />
    <template v-else>
      <template v-for="(block, blockIndex) in blocks" :key="blockIndex">
        <component :is="`h${block.level}`" v-if="block.type === 'heading'">
          <template
            v-for="(token, tokenIndex) in block.tokens"
            :key="tokenIndex"
          >
            <code v-if="token.type === 'code'">{{ token.text }}</code>
            <strong v-else-if="token.type === 'strong'">{{
              token.text
            }}</strong>
            <em v-else-if="token.type === 'emphasis'">{{ token.text }}</em>
            <a
              v-else-if="token.type === 'link'"
              :href="token.href"
              rel="noopener noreferrer"
              target="_blank"
              >{{ token.text }}</a
            >
            <span v-else>{{ token.text }}</span>
          </template>
        </component>
        <pre
          v-else-if="block.type === 'code'"
        ><code>{{ block.text }}</code></pre>
        <component
          :is="block.type === 'ordered-list' ? 'ol' : 'ul'"
          v-else-if="
            block.type === 'ordered-list' || block.type === 'unordered-list'
          "
        >
          <li v-for="(item, itemIndex) in block.items" :key="itemIndex">
            <template v-for="(token, tokenIndex) in item" :key="tokenIndex">
              <code v-if="token.type === 'code'">{{ token.text }}</code>
              <strong v-else-if="token.type === 'strong'">{{
                token.text
              }}</strong>
              <em v-else-if="token.type === 'emphasis'">{{ token.text }}</em>
              <a
                v-else-if="token.type === 'link'"
                :href="token.href"
                rel="noopener noreferrer"
                target="_blank"
                >{{ token.text }}</a
              >
              <span v-else>{{ token.text }}</span>
            </template>
          </li>
        </component>
        <blockquote v-else-if="block.type === 'quote'">
          <template
            v-for="(token, tokenIndex) in block.tokens"
            :key="tokenIndex"
          >
            <code v-if="token.type === 'code'">{{ token.text }}</code>
            <strong v-else-if="token.type === 'strong'">{{
              token.text
            }}</strong>
            <em v-else-if="token.type === 'emphasis'">{{ token.text }}</em>
            <a
              v-else-if="token.type === 'link'"
              :href="token.href"
              rel="noopener noreferrer"
              target="_blank"
              >{{ token.text }}</a
            >
            <span v-else>{{ token.text }}</span>
          </template>
        </blockquote>
        <div v-else-if="block.type === 'table'" class="markdown-table-wrap">
          <table>
            <thead>
              <tr>
                <th v-for="(cell, cellIndex) in block.header" :key="cellIndex">
                  <template
                    v-for="(token, tokenIndex) in cell"
                    :key="tokenIndex"
                  >
                    <code v-if="token.type === 'code'">{{ token.text }}</code>
                    <strong v-else-if="token.type === 'strong'">{{
                      token.text
                    }}</strong>
                    <em v-else-if="token.type === 'emphasis'">{{
                      token.text
                    }}</em>
                    <a
                      v-else-if="token.type === 'link'"
                      :href="token.href"
                      rel="noopener noreferrer"
                      target="_blank"
                      >{{ token.text }}</a
                    >
                    <span v-else>{{ token.text }}</span>
                  </template>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, rowIndex) in block.rows" :key="rowIndex">
                <td v-for="(cell, cellIndex) in row" :key="cellIndex">
                  <template
                    v-for="(token, tokenIndex) in cell"
                    :key="tokenIndex"
                  >
                    <code v-if="token.type === 'code'">{{ token.text }}</code>
                    <strong v-else-if="token.type === 'strong'">{{
                      token.text
                    }}</strong>
                    <em v-else-if="token.type === 'emphasis'">{{
                      token.text
                    }}</em>
                    <a
                      v-else-if="token.type === 'link'"
                      :href="token.href"
                      rel="noopener noreferrer"
                      target="_blank"
                      >{{ token.text }}</a
                    >
                    <span v-else>{{ token.text }}</span>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else-if="block.type === 'paragraph'">
          <template
            v-for="(token, tokenIndex) in block.tokens"
            :key="tokenIndex"
          >
            <code v-if="token.type === 'code'">{{ token.text }}</code>
            <strong v-else-if="token.type === 'strong'">{{
              token.text
            }}</strong>
            <em v-else-if="token.type === 'emphasis'">{{ token.text }}</em>
            <a
              v-else-if="token.type === 'link'"
              :href="token.href"
              rel="noopener noreferrer"
              target="_blank"
              >{{ token.text }}</a
            >
            <span v-else>{{ token.text }}</span>
          </template>
        </p>
      </template>
    </template>
  </div>
</template>

<style scoped>
.safe-markdown {
  min-width: 0;
  overflow-wrap: anywhere;
}
.safe-markdown :where(h1, h2, h3, h4) {
  margin: 14px 0 7px;
  font-size: 1rem;
  letter-spacing: 0;
}
.safe-markdown :where(p, ul, ol, blockquote) {
  margin: 7px 0;
}
.safe-markdown :where(ul, ol) {
  padding-left: 22px;
}
.safe-markdown blockquote {
  padding-left: 12px;
  border-left: 3px solid var(--border-strong, var(--border));
  color: var(--muted);
}
.safe-markdown code {
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--panel);
  font-family: "IBM Plex Mono", monospace;
}
.safe-markdown pre {
  max-width: 100%;
  padding: 12px;
  overflow: auto;
  border-radius: 6px;
  background: var(--panel);
  white-space: pre-wrap;
}
.safe-markdown pre code {
  padding: 0;
  background: transparent;
}
.markdown-table-wrap {
  max-width: 100%;
  overflow-x: auto;
}
.safe-markdown table {
  width: 100%;
  border-collapse: collapse;
}
.safe-markdown :where(th, td) {
  padding: 7px 9px;
  border: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}
</style>
