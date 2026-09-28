const maximumTitleBytes = 240;
const maximumTitleCharacters = 160;

export function runTitleFromTask(task: string): string {
  const source = task.trim();
  let title = "";
  let bytes = 0;
  let characters = 0;
  const encoder = new TextEncoder();
  for (const character of source) {
    const size = encoder.encode(character).length;
    if (
      characters >= maximumTitleCharacters ||
      bytes + size > maximumTitleBytes
    )
      break;
    title += character;
    bytes += size;
    characters += 1;
  }
  if (title.length < source.length) {
    const wordBoundary = title.lastIndexOf(" ");
    if (wordBoundary >= title.length * 0.6)
      title = title.slice(0, wordBoundary);
  }
  return title.trimEnd();
}
