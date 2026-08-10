const maximumPages = 100;

export async function collectAllPages<
  TPage extends { nextPageToken?: string },
  T,
>(
  loadPage: (pageToken?: string) => Promise<TPage>,
  items: (page: TPage) => readonly T[],
): Promise<{ values: T[]; pages: TPage[] }> {
  const pages: TPage[] = [];
  const values: T[] = [];
  let pageToken: string | undefined;
  const seen = new Set<string>();
  for (let index = 0; index < maximumPages; index += 1) {
    const page = await loadPage(pageToken);
    pages.push(page);
    values.push(...items(page));
    const next = page.nextPageToken;
    if (!next) return { values, pages };
    if (seen.has(next)) throw new Error("Pagination cursor loop detected");
    seen.add(next);
    pageToken = next;
  }
  throw new Error("Pagination page limit exceeded");
}
