#!/usr/bin/env node
// Побайтное сохранение журнала и проверка структурного переноса без GitHub/runtime effects.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
export const JOURNAL = 'docs/operations/self-development-dogfooding.md';
export const ARCHIVE = 'docs/operations/self-development-archive';
export const SOURCE_COMMIT = '892c029241548a887c8ff2459de8c461f69c367e';
export const BASE_COMMIT = 'cb611a2ed08def5071c2fc7c108c30ed3e63e03e';
export const SOURCE_SHA = '894ac60e3fb362f617961eca2e2d0aadb0d2b63d00f05bfe040a71d25e2c7df6';
export const SOURCE_BLOB = 'f84a5b11149a7c6b8a1fe4b4880b4db9d5e400c7';
export const PART_LIMIT = 256 * 1024;
export const ACTIVE_LIMIT = 128 * 1024;
export const ROLLING_MANIFEST = `${ARCHIVE}/checkpoints-manifest.json`;
const ROLLING_LIMIT = 256;
export const ACTIVE_DELIMITER = '\n## Текущие checkpoint\n\nОснастка проверяет сохранность текста, а не выполнение QA или выдачу полномочий.\nСмена checkbox требует внешних доказательств; PASS verifier не является approval.\nДатированная запись не переопределяет правила или закреплённые runtime inputs.\nБез предыдущего snapshot проверяются baseline и формат хвоста; доказательство\nappend-only относительно прежнего Git blob: `--verify --previous-revision <40hex SHA>`.\n\n<!-- OPS-DOC-SELFDEV-001:CURRENT-CHECKPOINTS:v1 -->\n';
export const EXTRACTS = [
  { key: 'dag', first: 1199, last: 1210, date: '08.10.2026 11:30 UTC', scope: 'Матрица DAG; exact39-stage относится к прежней публикации, не к текущему порядку review.' },
  { key: 'inputs-product', first: 1233, last: 1282, date: '08.10.2026 11:03 UTC', scope: 'Immutable inputs и рекомендованные продуктовые детали; исторический NOT RUN и отсутствие human approval сохранены.' },
  { key: 'autonomy', first: 2392, last: 2404, date: '08.10.2026 до 14:00 Саратов', scope: 'Датированное разрешение; срок не продлевается переносом. Защитные и визуальные ограничения сохранены.' },
  { key: 'intake', first: 3194, last: 3202, date: '07.10.2026 17:13 UTC', scope: 'Business output INTAKE и технические квитанции; исходный опубликованный процесс не переписывается.' },
  { key: 'own-catalog', first: 3370, last: 3378, date: '07.10.2026 16:23 UTC', scope: 'Собственный immutable catalog/entry, неизменённые границы и закрытый отказ.' },
  { key: 'github-eof', first: 5179, last: 5208, date: '07.10.2026 09:32–09:37 UTC', scope: 'Карта большого чтения; требование EOF и старые pins, не новое разрешение публикации.' },
  { key: 'file-receipt', first: 5707, last: 5735, date: '07.10.2026 06:47 UTC', scope: 'Матрица metadata-only квитанции; historical actual NOT RUN не повышается до PASS.' },
  { key: 'chronology', first: 8144, last: 8148, date: '04.10.2026, checkpoint 2c103867', scope: 'Полная хронология и bounded текст сообщения, а не служебный summary.' },
  { key: 'single-review', first: 12860, last: 12873, date: '08.10.2026 19:21 UTC', scope: 'Исходное решение об одном review; дата и прежний NOT CHANGED сохранены. Действующее решение — GOV-OD-003.' },
];
const MARKERS = /обязатель|запрещ|нельзя|долж[еа]н|требуется|требуют|требование|сохранять|не менять|не измен|не перепис|не подмен|не замен|не объяв|не выда|не счит|не разреш|не допуска|закрыто отклон|полномочи|authority|владелец.*(?:поруч|подтверд|разреш|приня)|Human Gate|до EOF|forward-only/iu;
const ROUTES = [
  [/review|ревью|реценз|приёмк|Human Gate|approval/iu, 'review-context', 'docs/governance/open-decisions.md#gov-od-003-число-рецензентов-в-локальном-dogfooding-1796'],
  [/полномоч|authority|eligibility|grant|secret|credential|TLS|SNI|policy|кэш|cache/iu, 'boundary-context', 'AGENTS.md'],
  [/EOF|catalog|каталог|immutable|snapshot|lease|cancel|terminal|retry|fence|cursor|attempt/iu, 'lifecycle-context', JOURNAL + '#карта-новых-пользовательских-сценариев'],
  [/screenshot|Chrome|Console|визуал|скриншот|компакт|интерфейс/iu, 'qa-context', 'docs/qa/full-qa-task.md'],
  [/PASS|FAIL|NOT RUN|UNKNOWN|NOT CAPTURED|NOT CHANGED|OPEN|BLOCKED/iu, 'dated-outcome', 'docs/governance/testing-strategy.md'],
];
export const sha256 = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');
export const blobSha = (bytes) => crypto.createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex');
export function validateSource(bytes) {
  if (bytes.length !== 1159280 || sha256(bytes) !== SOURCE_SHA || blobSha(bytes) !== SOURCE_BLOB) throw new Error('SOURCE_IDENTITY_MISMATCH');
  new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  if (bytes.includes(0) || bytes.includes(13) || bytes.at(-1) !== 10) throw new Error('SOURCE_ENCODING_INVALID');
}
function linesOf(bytes) {
  const ends = [0];
  for (let i = 0; i < bytes.length; i += 1) if (bytes[i] === 10) ends.push(i + 1);
  return { ends, lines: bytes.toString('utf8').split(/(?<=\n)/u) };
}
export function buildArtifacts(source) {
  validateSource(source);
  const { ends, lines } = linesOf(source);
  if (lines.length !== 13666) throw new Error('SOURCE_LINES_MISMATCH');
  const take = (first, last) => source.subarray(ends[first - 1], ends[last]);
  const record = (first, last) => ({ lines: [first, last], bytes: [ends[first - 1], ends[last]], sha256: sha256(take(first, last)) });
  const files = new Map();
  const parts = [];
  let start = 0;
  let first = 1;
  while (start < source.length) {
    let end = Math.min(start + PART_LIMIT, source.length);
    if (end < source.length) end = source.lastIndexOf(10, end - 1) + 1;
    if (end <= start) throw new Error('SOURCE_LINE_EXCEEDS_BOUND');
    const chunk = source.subarray(start, end);
    const last = first + chunk.reduce((n, b) => n + Number(b === 10), 0) - 1;
    const name = `892c0292-part-${String(parts.length + 1).padStart(3, '0')}.txt`;
    files.set(`${ARCHIVE}/${name}`, chunk);
    parts.push({ file: name, lines: [first, last], bytes: [start, end], size: chunk.length, sha256: sha256(chunk) });
    first = last + 1;
    start = end;
  }
  if (parts.length !== 5) throw new Error('ARCHIVE_PART_COUNT_INVALID');
  const provenance = `\n## Структурный перенос\n\nНовые правила/authority/approval не добавлены; статусы/checkbox сохранены.\nПлан/checklist/матрицы: строки4534–4802; checkpoint13–125 — по своим датам.\nИстёкшие автономные сроки не продлены.\nИсторические три reviews/exact39-stage не переопределяют\n[GOV-OD-003](../governance/open-decisions.md#gov-od-003-число-рецензентов-в-локальном-dogfooding-1796):\nодно комплексное review только #1796/#1797, приёмка Manager, финальный gate\nвладельца. Старые pins и ошибки сохранены.\n\nИсходник \`${SOURCE_COMMIT}\`: 1159280B/13666строк;\nSHA256 \`${SOURCE_SHA}\`.\n[Manifest](self-development-archive/manifest.json): hashes/заголовки;\n[покрытие](self-development-archive/coverage.json): абзацы/границы/скрытые нормы.\nЧасти — исходные UTF-8 bytes без новых DocID или разделителей.\n\n| Часть | Строки | Байты [начало,конец) |\n| --- | --- | --- |\n${parts.map(p => `| [${p.file}](self-development-archive/${p.file}) | ${p.lines.join('–')} | ${p.bytes.join('–')} |`).join('\n')}\n`;
  let active = take(1, 12).toString('utf8') + provenance + '\n## Канонический план с исходными доказательствами\n\n';
  active += take(4534, 4623).toString('utf8');
  active += '## Обязательный checklist\n\n';
  active += take(4624, 4802).toString('utf8');
  active += '\n## Дословные нормативные фрагменты исторических записей\n\n';
  active += 'Рекомендации не становятся approval, прежние публикации — текущими.\nДаты/NOT RUN сохранены; [AGENTS](../../AGENTS.md),\n[GOV-DOC-003](../governance/testing-strategy.md) и [Full65](../qa/full-qa-task.md)\nне переопределяются историей.\n\n';
  for (const item of EXTRACTS) {
    active += `### Строки${item.first}–${item.last}, ${item.date}\n\n`;
    active += take(item.first, item.last).toString('utf8') + '\n';
  }
  active += '\n## Журнал последних исходных checkpoint\n\n';
  active += take(13, 125).toString('utf8');
  active += 'Проверка: `node tools/dev/selfdev-journal-archive.mjs --verify`.\n';
  files.set(JOURNAL, Buffer.from(active));
  const headings = [];
  const contexts = ['Исходный документ до первого датированного checkpoint'];
  const paragraphs = [];
  let context = 0;
  let at = 0;
  while (at < lines.length) {
    const begin = at;
    const blank = lines[at].trim() === '';
    if (!blank) {
      while (at + 1 < lines.length && lines[at + 1].trim() !== '') at += 1;
    } else {
      while (at + 1 < lines.length && lines[at + 1].trim() === '') at += 1;
    }
    const lo = begin + 1, hi = at + 1;
    const text = take(lo, hi).toString('utf8');
    const firstLine = lines[begin].trim();
    if (/^#{1,6} /u.test(firstLine) || /^\d{2}\.\d{2}\.2026(?: |,)/u.test(firstLine)) {
      contexts.push(firstLine);
      context = contexts.length - 1;
      headings.push({ line: lo, context });
    }
    const exact = EXTRACTS.find(e => lo >= e.first && hi <= e.last);
    let category, target;
    if (hi <= 12) [category, target] = ['identity', JOURNAL];
    else if (lo >= 13 && hi <= 125) [category, target] = ['retained-checkpoint', JOURNAL];
    else if (lo >= 4534 && hi <= 4802) [category, target] = ['retained-canonical', JOURNAL];
    else if (exact) [category, target] = ['retained-fragment', exact.key];
    else if (blank) [category, target] = ['layout', 'archive'];
    else if (MARKERS.test(text)) {
      const route = ROUTES.find(([rx]) => rx.test(text));
      [category, target] = route ? ['dated-normative/' + route[1], route[2]] : ['dated-normative/scoped-implementation', JOURNAL];
    } else [category, target] = ['dated-evidence', 'archive'];
    // Наличие ключевого слова — указатель для контроля, не новая норма и не доказательство приёмки.
    paragraphs.push({ ...record(lo, hi), context, category, target, normativeMarker: MARKERS.test(text), parts: parts.filter(p => p.lines[0] <= hi && p.lines[1] >= lo).map(p => p.file) });
    at += 1;
  }
  const coverage = {
    format: 'selfdev-journal-coverage-v1', sourceSha256: SOURCE_SHA,
    classificationScope: 'Самостоятельное чтение источника до EOF. Датированные ограничения реализации не становятся новой authority; словарь только помечает дополнительные места для аудита. Все абзацы, включая немаркированные, побайтно доступны в архиве.',
    contexts, paragraphs,
  };
  files.set(`${ARCHIVE}/coverage.json`, Buffer.from(JSON.stringify(coverage, null, 2) + '\n'));
  const manifest = {
    format: 'selfdev-journal-archive-v1', baseCommit: BASE_COMMIT,
    source: { commit: SOURCE_COMMIT, path: JOURNAL, gitBlob: SOURCE_BLOB, sha256: SOURCE_SHA, size: source.length, lines: lines.length, encoding: 'UTF-8', lineEnding: 'LF' },
    reconstruction: 'В порядке parts: конкатенация исходных bytes без заголовков, разделителей и завершающих добавлений.',
    parts, active: { path: JOURNAL, size: Buffer.byteLength(active), sha256: sha256(Buffer.from(active)), unchangedRanges: [record(1, 12), record(4534, 4623), record(4624, 4802), record(13, 125)], fragments: EXTRACTS.map(e => ({ ...e, ...record(e.first, e.last) })) },
    coverage: { file: 'coverage.json', sha256: sha256(files.get(`${ARCHIVE}/coverage.json`)), paragraphs: paragraphs.length },
    semanticCoverage: [
      { family: 'Цель, решения владельца, checkbox1–16, четыре сценария и полные lifecycle матрицы', sourceLines: [4534, 4802], disposition: 'Дословно в активной части; временные разрешения и старые review отделены от GOV-OD-003.' },
      { family: 'Последние исходные статусы, backend freeze и независимость serving/live доказательств', sourceLines: [13, 125], disposition: 'Дословно в активном журнале по исходной дате, не актуальный readback нового runtime.' },
      { family: 'DAG Before/After/OCC/immutable, Workflow inputs, phase-specific outputs, own catalog и полный EOF', fragments: ['dag', 'inputs-product', 'intake', 'own-catalog', 'github-eof', 'file-receipt'], disposition: 'Дословные исторические фрагменты активны как источник формулировок; старый39-stage и NOT RUN остаются историей. Текущий порядок review задаёт GOV-OD-003.' },
      { family: 'Продуктовые рекомендации сводки лимитов: максимум3, группы исчерпанных, unknown/stale, min remainder, tie-break, supported credits/reset', sourceLines: [1263, 1282], disposition: 'Дословно; статус рекомендации и отсутствие human approval не повышены переносом.' },
      { family: 'Автономия, даты/условия разрешений и visual QA', fragments: ['autonomy'], disposition: 'Дословно с исходным сроком; не создаёт нового разрешения эффекта, продления автономии или отмены текущего freeze.' },
      { family: 'Полная хронология, safe metadata receipt и полный bounded текст сообщения', fragments: ['chronology', 'file-receipt'], disposition: 'Дословно; повторяющиеся исторические fixes остаются в архиве, core содержит общий lifecycle.' },
      { family: 'Одно комплексное review, Manager acceptance, пять циклов после первого, sole final owner gate', fragments: ['single-review'], disposition: 'Дословно и linked GOV-OD-003. Старые3/4 review,33/39-stage и ошибочные fixtures сохранены в архиве, не current authority.' },
      { family: 'Исторические решения конкретных implementation: URI/routes, budgets, thresholds, resources, loaded image/policy pins, migration/apply порядок, cleanup selectors', disposition: 'Датированные evidence/границы конкретного source. Все абзацы сохранены в coverage с архивными offsets; они не становятся новым правилом текущего workflow. Общие boundary/lifecycle/QA/proof требования направлены к действующим AGENTS, core, Full65 и GOV-DOC-003.' },
    ],
    historicalNorms: [
      { lines: [4553, 4555], status: 'Истёкший автономный срок, не продлён', active: true },
      { lines: [4566, 4567], status: 'Историческая модель нескольких review; локально заменена GOV-OD-003', active: true },
      { lines: [1284, 1293], status: 'Исторический предложенный39-stage порядок, не текущий21-stage', active: false },
      { lines: [10552, 10625], status: 'Датированные owner pause и последующее возобновление; не новый gate', active: false },
    ],
    headingIndex: headings,
    consumers: ['docs/governance/codification.md:131', 'docs/governance/open-decisions.md:59', 'docs/qa/full-qa-task.md:17', 'docs/operations/self-development-handoff.md:3838,3884,7009,7234', 'docs/operations/assistant-prototype-debug-1763.md:22,44'],
  };
  files.set(`${ARCHIVE}/manifest.json`, Buffer.from(JSON.stringify(manifest, null, 2) + '\n'));
  return files;
}
function readRegular(root, relative) {
  const file = path.resolve(root, relative);
  if (!file.startsWith(path.resolve(root) + path.sep)) throw new Error('ARCHIVE_PATH_INVALID');
  for (let parent = path.dirname(file); parent !== path.resolve(root); parent = path.dirname(parent)) {
    if (fs.lstatSync(parent).isSymbolicLink()) throw new Error('ARCHIVE_PATH_INVALID');
  }
  const stat = fs.lstatSync(file);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error('ARCHIVE_FILE_INVALID');
  return fs.readFileSync(file);
}
// Оснастка доказывает целостность текста, но не выполнение QA или выдачу полномочий.
// Checkbox меняется по внешним доказательствам; helper не подтверждает их достаточность.
function normalizeChecklist(text, baseline) {
  const start = baseline.indexOf('## Обязательный checklist\n\n');
  const end = baseline.indexOf('## Карта новых пользовательских сценариев\n', start);
  if (start < 0 || end < 0) throw new Error('CHECKLIST_BOUNDARY_INVALID');
  const chars = text.split('');
  for (const match of baseline.slice(start, end).matchAll(/^(\s*- \[)([ x])(\])/gmu)) {
    const offset = start + match.index + match[1].length;
    if (![' ', 'x'].includes(chars[offset])) throw new Error('CHECKLIST_STATE_INVALID');
    chars[offset] = match[2];
  }
  return chars.join('');
}
function validateCheckpointTail(tail) {
  if (tail === '') return;
  if (!tail.startsWith('\n## Checkpoint ') || !tail.endsWith('\n')) throw new Error('CHECKPOINT_APPEND_INVALID');
  let entries = 0, body = false, fence = null;
  for (const line of tail.split('\n')) {
    if (fence) {
      if (new RegExp(`^${fence}{3,}\\s*$`, 'u').test(line)) fence = null;
      else if (line.trim()) body = true;
      continue;
    }
    const marker = line.match(/^(?:```|~~~)/u);
    if (marker) { fence = marker[0][0]; continue; }
    if (line.startsWith('## Checkpoint ')) {
      if (entries > 0 && !body) throw new Error('CHECKPOINT_APPEND_INVALID');
      const heading = line.match(/^## Checkpoint (\d{2})\.(\d{2})\.(\d{4}) (\d{2}):(\d{2})(?:–(\d{2}):(\d{2}))? UTC — \S.*$/u);
      if (!heading) throw new Error('CHECKPOINT_APPEND_INVALID');
      const [, day, month, year, hour, minute, endHour, endMinute] = heading;
      const date = new Date(`${year}-${month}-${day}T${hour}:${minute}:00Z`);
      if (!Number.isFinite(date.valueOf()) || date.toISOString().slice(0, 16) !== `${year}-${month}-${day}T${hour}:${minute}` || (endHour && (+endHour > 23 || +endMinute > 59))) throw new Error('CHECKPOINT_DATE_INVALID');
      entries += 1; body = false;
    } else {
      if (/^#{1,2}\s|^---\s*$|^id:\s|[\x00-\x08\x0b-\x1f\x7f]/u.test(line)) throw new Error('CHECKPOINT_APPEND_INVALID');
      if (line.trim()) body = true;
    }
  }
  if (entries === 0 || !body || fence) throw new Error('CHECKPOINT_APPEND_INVALID');
}
function validateActiveLinks(text, root) {
  const local = /\[[^\]\n]*\]\(([^)\n]*)\)/gu;
  const matches = [...text.matchAll(local)];
  if ([...text.matchAll(/\]\(/gu)].length !== matches.length) throw new Error('ACTIVE_LINK_INVALID');
  for (const match of matches) {
    const href = match[1];
    if (/^https?:\/\/\S+$/u.test(href)) continue;
    if (!href || /^[a-z][a-z0-9+.-]*:/iu.test(href)) throw new Error('ACTIVE_LINK_INVALID');
    const [relative, anchor, extra] = href.split('#');
    if (extra !== undefined) throw new Error('ACTIVE_LINK_INVALID');
    const file = relative ? path.join(path.dirname(JOURNAL), relative) : JOURNAL;
    const bytes = readRegular(root, file);
    if (anchor) {
      const target = path.normalize(file) === JOURNAL ? text : bytes.toString('utf8');
      const anchors = [...target.matchAll(/^#{1,6}\s+(.+)$/gmu)].map(h => h[1].toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').replace(/\s/gu, '-'));
      if (!anchors.includes(anchor)) throw new Error('ACTIVE_LINK_INVALID');
    }
  }
}
export function verifyActive(active, baselineBytes, root = ROOT, previousActive) {
  if (active.length > ACTIVE_LIMIT) throw new Error('ACTIVE_SIZE_INVALID');
  const text = new TextDecoder('utf-8', { fatal: true }).decode(active);
  if (active.includes(0) || active.includes(13) || active.at(-1) !== 10) throw new Error('ACTIVE_ENCODING_INVALID');
  if ([...text.matchAll(/^id:\s*OPS-DOC-SELFDEV-001\s*$/gmu)].length !== 1 || [...text.matchAll(/^id:/gmu)].length !== 1) throw new Error('ACTIVE_DOCID_INVALID');
  const sections = text.split(ACTIVE_DELIMITER);
  if (sections.length !== 2) throw new Error('ACTIVE_DELIMITER_INVALID');
  const baseline = baselineBytes.toString('utf8');
  if (sections[0].length !== baseline.length) throw new Error('ACTIVE_BASELINE_MISMATCH');
  if (normalizeChecklist(sections[0], baseline) !== baseline) throw new Error('ACTIVE_BASELINE_MISMATCH');
  validateCheckpointTail(sections[1]);
  validateActiveLinks(text, root);
  if (previousActive !== undefined) {
    verifyActive(previousActive, baselineBytes, root);
    const previousTail = previousActive.toString('utf8').split(ACTIVE_DELIMITER)[1];
    if (!sections[1].startsWith(previousTail)) throw new Error('CHECKPOINT_HISTORY_REWRITTEN');
  }
}
// Отдельная цепочка checkpoint не меняет исходные пять частей и их manifest.
export function readRolling(root, baseline, active, read = relative => readRegular(root, relative), present = fs.existsSync(path.join(root, ROLLING_MANIFEST))) {
  const tail = Buffer.from(active.toString('utf8').split(ACTIVE_DELIMITER)[1]);
  if (!present) return { parts: [], files: [], history: tail, archived: Buffer.alloc(0) };
  const raw = read(ROLLING_MANIFEST);
  if (raw.length > 1024 * 1024) throw new Error('ROLLING_MANIFEST_SIZE_INVALID');
  const manifest = JSON.parse(raw);
  if (manifest.format !== 'selfdev-checkpoints-v1' || manifest.baselineSha256 !== sha256(baseline) || !Array.isArray(manifest.parts) || !manifest.parts.length || manifest.parts.length > ROLLING_LIMIT || !raw.equals(Buffer.from(JSON.stringify(manifest, null, 2) + '\n')) || Object.keys(manifest).sort().join(',') !== 'baselineSha256,format,parts') throw new Error('ROLLING_MANIFEST_INVALID');
  const files = [path.basename(ROLLING_MANIFEST)], chunks = [];
  let offset = 0;
  for (const [index, part] of manifest.parts.entries()) {
    if (part.file !== `checkpoints-${String(index + 1).padStart(3, '0')}.txt` || Object.keys(part).sort().join(',') !== 'bytes,file,sha256,size,source' || !Array.isArray(part.bytes) || part.bytes.length !== 2 || part.bytes[0] !== offset || !Number.isSafeInteger(part.size) || part.size <= 0 || part.size > PART_LIMIT || part.bytes[1] !== offset + part.size) throw new Error('ROLLING_PART_INVALID');
    const bytes = read(`${ARCHIVE}/${part.file}`);
    if (bytes.length !== part.size || sha256(bytes) !== part.sha256) throw new Error('ROLLING_PART_IDENTITY_MISMATCH');
    new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    validateCheckpointTail(bytes.toString('utf8'));
    files.push(part.file);
    chunks.push(bytes);
    offset += bytes.length;
  }
  const archived = Buffer.concat(chunks), history = Buffer.concat([archived, tail]);
  validateCheckpointTail(history.toString('utf8'));
  for (const part of manifest.parts) {
    const pin = part.source;
    if (!pin || Object.keys(pin).sort().join(',') !== 'file,gitBlob,historyEnd,historyStart,revision,sha256,size' || !/^[a-f0-9]{40}$/u.test(pin.revision) || !/^[a-f0-9]{40}$/u.test(pin.gitBlob) || pin.file !== `checkpoint-source-${pin.gitBlob}.txt` || pin.historyStart !== part.bytes[0] || !Number.isSafeInteger(pin.historyEnd) || pin.historyEnd < part.bytes[1] || pin.historyEnd > history.length || !Number.isSafeInteger(pin.size) || pin.size <= 0 || pin.size > ACTIVE_LIMIT) throw new Error('ROLLING_SOURCE_PIN_INVALID');
    const snapshot = read(`${ARCHIVE}/${pin.file}`);
    if (snapshot.length !== pin.size || sha256(snapshot) !== pin.sha256 || blobSha(snapshot) !== pin.gitBlob) throw new Error('ROLLING_SOURCE_IDENTITY_MISMATCH');
    verifyActive(snapshot, baseline, root);
    const previousTail = Buffer.from(snapshot.toString('utf8').split(ACTIVE_DELIMITER)[1]);
    if (!previousTail.equals(history.subarray(pin.historyStart, pin.historyEnd))) throw new Error('CHECKPOINT_HISTORY_REWRITTEN');
    files.push(pin.file);
  }
  return { parts: manifest.parts, files: [...new Set(files)], history, archived };
}
export function verifyArtifacts(root = ROOT, previousActive, previousArchived = Buffer.alloc(0)) {
  const manifest = JSON.parse(readRegular(root, `${ARCHIVE}/manifest.json`));
  if (manifest.format !== 'selfdev-journal-archive-v1' || manifest.parts.length !== 5) throw new Error('MANIFEST_INVALID');
  const chunks = manifest.parts.map((p, index) => {
    if (p.file !== `892c0292-part-${String(index + 1).padStart(3, '0')}.txt`) throw new Error('PART_ORDER_INVALID');
    const bytes = readRegular(root, `${ARCHIVE}/${p.file}`);
    new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    if (bytes.length > PART_LIMIT || bytes.length !== p.size || sha256(bytes) !== p.sha256) throw new Error('PART_IDENTITY_MISMATCH');
    return bytes;
  });
  const source = Buffer.concat(chunks);
  validateSource(source);
  const expected = buildArtifacts(source);
  const names = [...expected.keys()].filter(p => p.startsWith(ARCHIVE + '/')).map(p => path.basename(p)).sort();
  for (const [relative, bytes] of expected) if (relative !== JOURNAL && !readRegular(root, relative).equals(bytes)) throw new Error(`ARTIFACT_MISMATCH:${relative}`);
  const active = readRegular(root, JOURNAL);
  verifyActive(active, expected.get(JOURNAL), root);
  const rolling = readRolling(root, expected.get(JOURNAL), active);
  const inventory = [...names, ...rolling.files].sort();
  if (JSON.stringify(fs.readdirSync(path.join(root, ARCHIVE)).sort()) !== JSON.stringify(inventory)) throw new Error('ARCHIVE_INVENTORY_INVALID');
  if (previousActive !== undefined) {
    verifyActive(previousActive, expected.get(JOURNAL), root);
    const previousTail = Buffer.from(previousActive.toString('utf8').split(ACTIVE_DELIMITER)[1]);
    const prefix = Buffer.concat([previousArchived, previousTail]);
    if (!rolling.history.subarray(0, prefix.length).equals(prefix)) throw new Error('CHECKPOINT_HISTORY_REWRITTEN');
  }
  for (const relative of names) if (readRegular(root, `${ARCHIVE}/${relative}`).length > 1024 * 1024) throw new Error('ARCHIVE_METADATA_SIZE_INVALID');
  return { sourceBytes: source.length, activeBytes: active.length, parts: chunks.length, sourceSha256: SOURCE_SHA, files: expected.size + rolling.files.length, checkpointHistory: previousActive === undefined ? 'BASELINE_AND_APPEND_FORMAT_ONLY' : 'PREVIOUS_SNAPSHOT_PREFIX_CHECKED' };
}
export function buildRollingArtifacts(root, previousActive, revision, previousArchived = Buffer.alloc(0)) {
  if (!/^[a-f0-9]{40}$/u.test(revision)) throw new Error('ROLLING_REVISION_INVALID');
  verifyArtifacts(root, previousActive, previousArchived);
  const active = readRegular(root, JOURNAL);
  if (!active.equals(previousActive)) throw new Error('ROLLING_PREIMAGE_MISMATCH');
  const original = JSON.parse(readRegular(root, `${ARCHIVE}/manifest.json`));
  const baseline = buildArtifacts(Buffer.concat(original.parts.map(p => readRegular(root, `${ARCHIVE}/${p.file}`)))).get(JOURNAL);
  const rolling = readRolling(root, baseline, active);
  const [prefix, tail] = active.toString('utf8').split(ACTIVE_DELIMITER);
  let fence = null, last = 0, at = 0;
  for (const line of tail.split(/(?<=\n)/u)) {
    if (fence) {
      if (new RegExp(`^${fence}{3,}\\s*$`, 'u').test(line.trimEnd())) fence = null;
    } else if (/^(?:```|~~~)/u.test(line)) fence = line[0];
    else if (line.startsWith('## Checkpoint ')) last = at - 1;
    at += line.length;
  }
  if (last <= 0 || rolling.parts.length >= ROLLING_LIMIT) throw new Error('ROLLING_NOTHING_TO_ARCHIVE');
  const bytes = Buffer.from(tail.slice(0, last)), nextActive = Buffer.from(prefix + ACTIVE_DELIMITER + tail.slice(last));
  verifyActive(nextActive, baseline, root);
  validateCheckpointTail(bytes.toString('utf8'));
  const file = `checkpoints-${String(rolling.parts.length + 1).padStart(3, '0')}.txt`;
  const sourceFile = `checkpoint-source-${blobSha(active)}.txt`;
  const part = { file, bytes: [rolling.archived.length, rolling.archived.length + bytes.length], size: bytes.length, sha256: sha256(bytes), source: { revision, file: sourceFile, gitBlob: blobSha(active), sha256: sha256(active), size: active.length, historyStart: rolling.archived.length, historyEnd: rolling.history.length } };
  const manifest = { format: 'selfdev-checkpoints-v1', baselineSha256: sha256(baseline), parts: [...rolling.parts, part] };
  const files = new Map([[JOURNAL, nextActive], [`${ARCHIVE}/${file}`, bytes], [`${ARCHIVE}/${sourceFile}`, active], [ROLLING_MANIFEST, Buffer.from(JSON.stringify(manifest, null, 2) + '\n')]]);
  return files;
}
// CLI сверяет provenance с Git, а не только с самим manifest.
export function verifySourceGitPins(parts, resolveBlob) {
  for (const part of parts) {
    if (resolveBlob(part.source.revision) !== part.source.gitBlob) throw new Error('ROLLING_GIT_PREIMAGE_MISMATCH');
  }
}
function verifyCurrentGitPins(root) {
  if (!fs.existsSync(path.join(root, ROLLING_MANIFEST))) return;
  const manifest = JSON.parse(readRegular(root, ROLLING_MANIFEST));
  verifySourceGitPins(manifest.parts, revision => execFileSync('git', ['rev-parse', `${revision}:${JOURNAL}`], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim());
}
function previousSnapshot(root, revision) {
  const read = relative => execFileSync('git', ['show', `${revision}:${relative}`], { cwd: root, maxBuffer: 1024 * 1024, stdio: ['ignore', 'pipe', 'ignore'] });
  const active = read(JOURNAL);
  const baseline = buildArtifacts(Buffer.concat(JSON.parse(read(`${ARCHIVE}/manifest.json`)).parts.map(p => read(`${ARCHIVE}/${p.file}`)))).get(JOURNAL);
  verifyActive(active, baseline, root);
  const present = execFileSync('git', ['ls-tree', '--name-only', revision, '--', ROLLING_MANIFEST], { cwd: root, encoding: 'utf8' }).trim() === ROLLING_MANIFEST;
  return { active, archived: readRolling(root, baseline, active, read, present).archived };
}
export function assertWriteTarget(root, head) {
  const real = fs.realpathSync(root);
  if (!real.startsWith('/home/s/.cache/kodex-selfdev-split.') || path.basename(real) !== 'source') throw new Error('ISOLATED_TARGET_REQUIRED');
  if (head !== BASE_COMMIT) throw new Error('BASE_COMMIT_MISMATCH');
}
function writeArtifacts() {
  const head = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: ROOT, encoding: 'utf8' }).trim();
  assertWriteTarget(ROOT, head);
  const source = execFileSync('git', ['show', `${SOURCE_COMMIT}:${JOURNAL}`], { cwd: ROOT, maxBuffer: 2 * 1024 * 1024 });
  validateSource(source);
  const artifacts = buildArtifacts(source);
  if (fs.existsSync(path.join(ROOT, ARCHIVE))) {
    const dir = fs.lstatSync(path.join(ROOT, ARCHIVE));
    if (!dir.isDirectory() || dir.isSymbolicLink()) throw new Error('TARGET_PREIMAGE_MISMATCH');
    const old = JSON.parse(readRegular(ROOT, `${ARCHIVE}/manifest.json`));
    const active = readRegular(ROOT, JOURNAL);
    const baseline = artifacts.get(JOURNAL);
    if (old.source?.sha256 !== SOURCE_SHA || sha256(baseline) !== old.active?.sha256 || (!active.equals(baseline) && !active.equals(Buffer.concat([baseline, Buffer.from(ACTIVE_DELIMITER)])))) throw new Error('TARGET_PREIMAGE_MISMATCH');
    const partNames = [...artifacts.keys()].filter(p => p.endsWith('.txt'));
    for (const relative of partNames) if (!readRegular(ROOT, relative).equals(artifacts.get(relative))) throw new Error('TARGET_PREIMAGE_MISMATCH');
  } else {
    if (!readRegular(ROOT, JOURNAL).equals(source)) throw new Error('TARGET_PREIMAGE_MISMATCH');
    fs.mkdirSync(path.join(ROOT, ARCHIVE));
  }
  for (const [relative, bytes] of artifacts) {
    if (relative === JOURNAL) continue;
    if (relative.endsWith('.txt') && fs.existsSync(path.join(ROOT, relative))) continue;
    fs.writeFileSync(path.join(ROOT, relative), bytes, { flag: relative.endsWith('.txt') ? 'wx' : 'w' });
  }
  fs.writeFileSync(path.join(ROOT, JOURNAL), Buffer.concat([artifacts.get(JOURNAL), Buffer.from(ACTIVE_DELIMITER)]));
  return verifyArtifacts();
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const command = process.argv[2] ?? '--verify';
    const previousRevision = process.argv[4];
    if (!['--verify', '--write', '--roll-plan'].includes(command) || (command === '--roll-plan' && !previousRevision) || (process.argv.length > 3 && (command === '--write' || process.argv.length !== 5 || process.argv[3] !== '--previous-revision' || !/^[0-9a-f]{40}$/u.test(previousRevision)))) throw new Error('COMMAND_INVALID');
    const previous = previousRevision ? previousSnapshot(ROOT, previousRevision) : undefined;
    const result = command === '--write' ? writeArtifacts() : command === '--roll-plan' ? { mode: 'WRITE_FREE_ROLL_PLAN', files: [...buildRollingArtifacts(ROOT, previous.active, previousRevision, previous.archived)].map(([file, bytes]) => ({ file, sha256: sha256(bytes), size: bytes.length, text: bytes.toString('utf8') })) } : verifyArtifacts(ROOT, previous?.active, previous?.archived);
    if (command !== '--write') verifyCurrentGitPins(ROOT);
    process.stdout.write(JSON.stringify(result) + '\n');
  } catch (error) {
    process.stderr.write(`Journal archive verification failed: ${error.message}\n`);
    process.exitCode = 1;
  }
}
