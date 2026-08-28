const opaqueRefPattern =
  /\b(?:agt|art|bld|cap|cnv|con|edg|evt|gat|inc|int|job|mbr|msg|nod|pln|prj|rev|rol|rti|run|sch|ses|trn|usr|wfl)_[A-Za-z0-9_-]{8,}\b/g;

export interface SafeSummaryValue {
  text: string;
  structured: boolean;
  truncated: boolean;
}

function looksStructured(value: string): boolean {
  if (!value.startsWith("{") && !value.startsWith("[")) return false;
  try {
    const parsed: unknown = JSON.parse(value);
    return typeof parsed === "object" && parsed !== null;
  } catch {
    return value.endsWith("}") || value.endsWith("]");
  }
}

export function safeSummary(
  value: string | null | undefined,
  maximumLength = 180,
): SafeSummaryValue {
  const source = value?.trim() ?? "";
  if (!source) return { text: "", structured: false, truncated: false };
  if (looksStructured(source))
    return { text: "", structured: true, truncated: false };

  const normalized = source
    .replace(/```(?:\w+)?\s*([\s\S]*?)```/g, "$1")
    .replace(/!\[([^\]]*)]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)]\([^)]*\)/g, "$1")
    .replace(opaqueRefPattern, "")
    .replace(/(^|\s)#{1,6}\s+/g, "$1")
    .replace(/[*_~`>]/g, "")
    .replace(/\s+/g, " ")
    .replace(/\s+([.,!?;:])/g, "$1")
    .trim();
  if (normalized.length <= maximumLength)
    return { text: normalized, structured: false, truncated: false };
  const slice = Array.from(normalized)
    .slice(0, maximumLength - 1)
    .join("");
  return {
    text: `${slice.replace(/\s+\S*$/, "").trimEnd()}…`,
    structured: false,
    truncated: true,
  };
}
