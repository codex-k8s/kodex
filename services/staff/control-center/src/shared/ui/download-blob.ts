// Скачивает уже полученный Blob через скрытую ссылку и освобождает object URL после click.
// Доступ к artifact, ожидание ответа и обработка ошибок остаются у вызывающего компонента.
export function downloadBlob(body: Blob, fileName: string): void {
  const url = URL.createObjectURL(body);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  anchor.hidden = true;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
