function codes(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

export function primarySttLanguage(
  stt: Record<string, unknown>,
  multiple: boolean,
): string {
  const language = typeof stt.language === "string" ? stt.language : "";
  if (!multiple) return language || codes(parameters(stt).languages)[0] || "";
  return codes(parameters(stt).languages)[0] || language;
}

export function additionalSttLanguages(stt: Record<string, unknown>): string {
  return codes(parameters(stt).languages).slice(1).join("\n");
}

export function setPrimarySttLanguage(
  stt: Record<string, unknown>,
  multiple: boolean,
  code: string,
): Record<string, unknown> {
  const current = parameters(stt);
  return {
    ...stt,
    language: multiple ? "" : code,
    parameters: {
      ...current,
      languages: multiple
        ? code
          ? [code, ...codes(current.languages).slice(1)]
          : []
        : [],
    },
  };
}

export function setAdditionalSttLanguages(
  stt: Record<string, unknown>,
  value: string,
): Record<string, unknown> {
  const current = parameters(stt);
  const primary = primarySttLanguage(stt, true);
  return {
    ...stt,
    language: "",
    parameters: {
      ...current,
      languages: [primary, ...value.split("\n").map((code) => code.trim())]
        .filter(Boolean)
        .filter((code, index, all) => all.indexOf(code) === index),
    },
  };
}

function parameters(stt: Record<string, unknown>): Record<string, unknown> {
  return stt.parameters &&
    typeof stt.parameters === "object" &&
    !Array.isArray(stt.parameters)
    ? (stt.parameters as Record<string, unknown>)
    : {};
}
