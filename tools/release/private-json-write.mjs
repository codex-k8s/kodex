import { constants, closeSync, fsyncSync, openSync, writeSync } from "node:fs";
import { open } from "node:fs/promises";

// Сохраняет прежнюю pretty JSON-форму: wx/0600, один writeSync, fsync и close.
// Валидация пути, содержимого и привязки к плану остаётся у release-сценария.
export function createPrivatePrettyJSONSync(path, value) {
  const fd = openSync(
    path,
    constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL,
    0o600,
  );
  try {
    writeSync(fd, JSON.stringify(value, null, 2) + "\n");
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
}

// Новый журнал создаётся эксклюзивно; продолжение не создаёт файл и не следует
// symlink. Права/identity существующего журнала проверяет вызывающий сценарий.
export function appendPrivateJSONLineSync(path, value, newFile = false) {
  const fd = openSync(
    path,
    newFile
      ? constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL
      : constants.O_WRONLY | constants.O_APPEND | constants.O_NOFOLLOW,
    0o600,
  );
  try {
    writeSync(fd, JSON.stringify(value) + "\n");
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
}

// Асинхронные release receipts сохраняют компактный JSON, а не pretty форму.
// sync относится к файлу; подтверждение операции и долговечность каталога не обещаются.
export async function createPrivateJSON(path, value) {
  const file = await open(path, "wx", 0o600);
  try {
    await file.writeFile(JSON.stringify(value) + "\n");
    await file.sync();
  } finally {
    await file.close();
  }
}
