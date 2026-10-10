// Ограниченная локальная проверка покрытия; не заменяет native/live приёмку.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import {
  ROOT, JOURNAL, ARCHIVE, SOURCE_COMMIT, BASE_COMMIT, SOURCE_SHA, SOURCE_BLOB,
  PART_LIMIT, ACTIVE_LIMIT, ACTIVE_DELIMITER, EXTRACTS, ROLLING_MANIFEST, readRolling, buildRollingArtifacts, verifySourceGitPins, sha256, blobSha, validateSource, buildArtifacts, verifyActive, verifyArtifacts, assertWriteTarget,
} from './selfdev-journal-archive.mjs';

const manifest = JSON.parse(fs.readFileSync(path.join(ROOT, ARCHIVE, 'manifest.json')));
const chunks = manifest.parts.map(p => fs.readFileSync(path.join(ROOT, ARCHIVE, p.file)));
const source = Buffer.concat(chunks);
const sourceLines = source.toString('utf8').split(/(?<=\n)/u);
const active = fs.readFileSync(path.join(ROOT, JOURNAL), 'utf8');
const coverage = JSON.parse(fs.readFileSync(path.join(ROOT, ARCHIVE, 'coverage.json')));
const slice = (lo, hi) => sourceLines.slice(lo - 1, hi).join('');
const artifacts = buildArtifacts(source);
const baseline = artifacts.get(JOURNAL);
const baselineText = baseline.toString('utf8');

test('Git provenance проверяется независимо от самосогласованного manifest', () => {
  const revision = 'a'.repeat(40), gitBlob = blobSha(Buffer.from(active));
  const parts = [{ source: { revision, gitBlob } }];
  assert.doesNotThrow(() => verifySourceGitPins(parts, ref => {
    assert.equal(ref, revision);
    return gitBlob;
  }));
  assert.throws(() => verifySourceGitPins(parts, () => 'b'.repeat(40)), /ROLLING_GIT_PREIMAGE_MISMATCH/u);
  assert.throws(() => verifySourceGitPins(parts, () => { throw new Error('Git revision unavailable'); }), /Git revision unavailable/u);
});

test('Публичная проверка завершает полный bounded перенос', () => {
  assert.deepEqual(verifyArtifacts(), {
    sourceBytes: 1159280, activeBytes: Buffer.byteLength(active), parts: 5,
    sourceSha256: SOURCE_SHA, files: 8 + readRolling(ROOT, baseline, Buffer.from(active)).files.length, checkpointHistory: 'BASELINE_AND_APPEND_FORMAT_ONLY',
  });
});
test('Механическая запись запрещена в основном checkout и на другом base SHA', () => {
  assert.throws(() => assertWriteTarget('/home/s/projects/kodex', BASE_COMMIT), /ISOLATED_TARGET_REQUIRED/u);
  const temporary = fs.mkdtempSync('/home/s/.cache/kodex-selfdev-split.');
  try {
    const root = path.join(temporary, 'source');
    fs.mkdirSync(root);
    assert.throws(() => assertWriteTarget(root, SOURCE_COMMIT), /BASE_COMMIT_MISMATCH/u);
    assertWriteTarget(root, BASE_COMMIT);
  } finally {
    fs.rmSync(temporary, { recursive: true });
  }
});
test('Пять частей восстанавливают исходный Git blob и SHA256 без потери bytes', () => {
  validateSource(source);
  assert.equal(sha256(source), SOURCE_SHA);
  assert.equal(blobSha(source), SOURCE_BLOB);
  assert.equal(sourceLines.length, 13666);
  const gitSource = execFileSync('git', ['show', `${SOURCE_COMMIT}:${JOURNAL}`], { cwd: ROOT, maxBuffer: 2 * 1024 * 1024 });
  assert.ok(source.equals(gitSource));
  const baseSource = execFileSync('git', ['show', `${BASE_COMMIT}:${JOURNAL}`], { cwd: ROOT, maxBuffer: 2 * 1024 * 1024 });
  assert.ok(source.equals(baseSource));
});
test('Размер, UTF8, LF, hashes и offsets каждой части связаны без gap/overlap', () => {
  let offset = 0, line = 1;
  for (const [index, part] of manifest.parts.entries()) {
    const bytes = chunks[index];
    assert.ok(bytes.length <= PART_LIMIT);
    assert.equal(bytes.length, part.size);
    assert.equal(sha256(bytes), part.sha256);
    assert.equal(part.bytes[0], offset);
    assert.equal(part.bytes[1], offset + bytes.length);
    assert.equal(part.lines[0], line);
    assert.equal(part.lines[1], line + bytes.reduce((n, b) => n + Number(b === 10), 0) - 1);
    assert.equal(bytes.at(-1), 10);
    assert.ok(!bytes.includes(0) && !bytes.includes(13));
    new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    offset = part.bytes[1]; line = part.lines[1] + 1;
  }
  assert.equal(offset, source.length);
  assert.equal(line, 13667);
});
test('Baseline дословен; active сохраняет неизменные фрагменты и разрешённые текущие checkbox', () => {
  for (const [lo, hi] of [[1, 12], [4534, 4623], [4624, 4802], [13, 125]]) assert.ok(baselineText.includes(slice(lo, hi)), `${lo}-${hi}`);
  for (const [lo, hi] of [[1, 12], [4534, 4623], [4733, 4802], [13, 125]]) assert.ok(active.includes(slice(lo, hi)), `${lo}-${hi}`);
  for (const e of EXTRACTS) {
    assert.ok(baselineText.includes(slice(e.first, e.last)), e.key);
    assert.ok(active.includes(slice(e.first, e.last)), e.key);
    const record = manifest.active.fragments.find(f => f.key === e.key);
    assert.equal(record.sha256, sha256(Buffer.from(slice(e.first, e.last))));
  }
  const boxes = text => [...text.matchAll(/^- \[[ x]\].*$/gmu)].map(m => m[0]);
  assert.deepEqual(boxes(baselineText), boxes(slice(4534, 4802)));
  assert.equal(baseline.length, manifest.active.size);
  assert.equal(sha256(baseline), manifest.active.sha256);
  assert.doesNotThrow(() => verifyActive(Buffer.from(active), baseline));
  assert.ok(Buffer.byteLength(active) >= 55 * 1024 && Buffer.byteLength(active) <= ACTIVE_LIMIT);
  assert.ok(active.includes('## Обязательный checklist'));
  assert.ok(active.includes('Исторические три reviews/exact39-stage не переопределяют'));
  assert.ok(active.includes('status: approved\n'));
});
test('Карта покрывает каждый исходный byte/line, включая все unheaded paragraphs и скрытые нормы', () => {
  let offset = 0, line = 1;
  for (const p of coverage.paragraphs) {
    assert.equal(p.bytes[0], offset);
    assert.equal(p.lines[0], line);
    const bytes = source.subarray(...p.bytes);
    assert.equal(bytes.toString('utf8'), slice(...p.lines));
    assert.equal(sha256(bytes), p.sha256);
    assert.ok(p.category && p.target && coverage.contexts[p.context]);
    assert.deepEqual(p.parts, manifest.parts.filter(c => c.lines[0] <= p.lines[1] && c.lines[1] >= p.lines[0]).map(c => c.file));
    if (p.normativeMarker) assert.notEqual(p.category, 'dated-evidence');
    offset = p.bytes[1]; line = p.lines[1] + 1;
  }
  assert.equal(offset, source.length); assert.equal(line, 13667);
  assert.equal(sha256(fs.readFileSync(path.join(ROOT, ARCHIVE, 'coverage.json'))), manifest.coverage.sha256);
  assert.equal(coverage.paragraphs.length, manifest.coverage.paragraphs);
  assert.ok(coverage.paragraphs.some(p => p.lines[0] >= 7624 && p.lines[1] <= 11131 && p.normativeMarker));
  for (const h of manifest.headingIndex) assert.equal(sourceLines[h.line - 1].trim(), coverage.contexts[h.context]);
});
test('Классификация скрытых норм ссылается на существующие канонические files/anchors', () => {
  const targets = new Set(coverage.paragraphs.filter(p => p.category.startsWith('dated-normative/')).map(p => p.target));
  for (const target of targets) {
    const [file, anchor] = target.split('#');
    const absolute = path.resolve(ROOT, file);
    assert.ok(absolute.startsWith(ROOT + path.sep));
    assert.ok(fs.statSync(absolute).isFile(), target);
    if (anchor) {
      const headings = [...fs.readFileSync(absolute, 'utf8').matchAll(/^#{1,6}\s+(.+)$/gmu)].map(h => h[1].toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').replace(/\s/gu, '-'));
      assert.ok(headings.includes(anchor), target);
    }
  }
});
function markdownFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap(e => {
    if (e.isSymbolicLink() || e.name === 'node_modules' || e.name === '.git') return [];
    const p = path.join(dir, e.name);
    return e.isDirectory() ? markdownFiles(p) : e.name.endsWith('.md') ? [p] : [];
  });
}
test('DocID OPS001 единственный; registry/consumer paths остаются прежними', () => {
  const files = [...markdownFiles(path.join(ROOT, 'docs')), path.join(ROOT, 'AGENTS.md')];
  const matches = files.filter(p => /^id: OPS-DOC-SELFDEV-001$/mu.test(fs.readFileSync(p, 'utf8')));
  assert.deepEqual(matches, [path.join(ROOT, JOURNAL)]);
  const registry = fs.readFileSync(path.join(ROOT, 'docs/governance/codification.md'), 'utf8');
  assert.equal([...registry.matchAll(/OPS-DOC-SELFDEV-001/gu)].length, 1);
  assert.ok(registry.includes(JOURNAL));
  for (const consumer of manifest.consumers) assert.ok(fs.statSync(path.join(ROOT, consumer.split(':')[0])).isFile());
  assert.ok(!fs.readdirSync(path.join(ROOT, ARCHIVE)).some(p => p.endsWith('.md')));
});
test('Все local Markdown links активной каноники разрешаются; GOV-OD003 anchor сохранён', () => {
  for (const m of active.matchAll(/\[[^\]]*\]\(([^)]+)\)/gu)) {
    const href = m[1];
    if (/^(?:https?:|mailto:)/u.test(href)) continue;
    const [file, anchor] = href.split('#');
    const absolute = path.resolve(ROOT, path.dirname(JOURNAL), file || path.basename(JOURNAL));
    assert.ok(absolute.startsWith(ROOT + path.sep));
    assert.ok(fs.statSync(absolute).isFile(), href);
    if (anchor) {
      const text = fs.readFileSync(absolute, 'utf8');
      const anchors = [...text.matchAll(/^#{1,6}\s+(.+)$/gmu)].map(h => h[1].toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').replace(/\s/gu, '-'));
      assert.ok(anchors.includes(anchor), href);
    }
  }
  assert.ok(manifest.headingIndex.some(h => coverage.contexts[h.context].includes('17:13')));
  assert.ok(manifest.headingIndex.some(h => coverage.contexts[h.context].includes('04:23–04:30')));
});
test('Архив хранит отменённые review, ошибочный RESTORE и их последующие исправления', () => {
  assert.ok(source.toString('utf8').includes('Внутренние Documentation/Security/Lexical reviews — обязательная часть'));
  assert.ok(source.toString('utf8').includes('Предыдущее утверждение журнала о\nrunner10001 относится к историческому ошибочному fixture'));
  assert.ok(source.toString('utf8').includes('Предыдущий supply-chain apply дошёл до migration и применения policy'));
  assert.ok(source.toString('utf8').includes('Владелец 08.10.2026 принял явное исключение для нового текущего живого'));
});
function withFixture(fn) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'kodex-journal-fixture-'));
  try {
    fs.mkdirSync(path.join(root, ARCHIVE), { recursive: true });
    for (const [relative, bytes] of artifacts) fs.writeFileSync(path.join(root, relative), relative === JOURNAL ? Buffer.concat([bytes, Buffer.from(ACTIVE_DELIMITER)]) : bytes);
    for (const match of baseline.toString('utf8').matchAll(/\[[^\]]*\]\(([^)]+)\)/gu)) {
      if (/^https?:/u.test(match[1])) continue;
      const relative = path.normalize(path.join(path.dirname(JOURNAL), match[1].split('#')[0]));
      if (relative.startsWith(ARCHIVE + '/')) continue;
      fs.mkdirSync(path.dirname(path.join(root, relative)), { recursive: true });
      fs.copyFileSync(path.join(ROOT, relative), path.join(root, relative));
    }
    fn(root);
  } finally {
    fs.rmSync(root, { recursive: true });
  }
}
test('Тампер каждой части закрыто отклоняется', () => {
  for (let index = 0; index < 5; index += 1) withFixture(root => {
    const p = path.join(root, ARCHIVE, manifest.parts[index].file);
    const b = fs.readFileSync(p); b[0] ^= 1; fs.writeFileSync(p, b);
    assert.throws(() => verifyArtifacts(root));
  });
});
test('Перестановка/отсутствие/лишний byte/invalidUTF8/oversize закрыто отклоняются', () => {
  for (const mutate of [
    root => fs.renameSync(path.join(root, ARCHIVE, manifest.parts[0].file), path.join(root, ARCHIVE, 'missing.txt')),
    root => fs.writeFileSync(path.join(root, ARCHIVE, manifest.parts[0].file), chunks[1]),
    root => fs.appendFileSync(path.join(root, ARCHIVE, manifest.parts[4].file), '\n'),
    root => fs.writeFileSync(path.join(root, ARCHIVE, manifest.parts[0].file), Buffer.from([0xff])),
    root => fs.writeFileSync(path.join(root, ARCHIVE, manifest.parts[0].file), Buffer.alloc(PART_LIMIT + 1)),
  ]) withFixture(root => { mutate(root); assert.throws(() => verifyArtifacts(root)); });
});
test('Подмена manifest/coverage/недопустимого checkbox и symlink вместо части закрыто отклоняются', () => {
  for (const mutate of [
    root => { const m = structuredClone(manifest); m.parts.reverse(); fs.writeFileSync(path.join(root, ARCHIVE, 'manifest.json'), JSON.stringify(m)); },
    root => fs.writeFileSync(path.join(root, ARCHIVE, 'coverage.json'), '{}\n'),
    root => fs.writeFileSync(path.join(root, JOURNAL), baselineText.replace('- [ ] 11.', '- [X] 11.') + ACTIVE_DELIMITER),
    root => { const p = path.join(root, ARCHIVE, manifest.parts[0].file); fs.renameSync(p, p + '.original'); fs.symlinkSync(p + '.original', p); },
  ]) withFixture(root => { mutate(root); assert.throws(() => verifyArtifacts(root)); });
});
const checkpoint = '\n## Checkpoint 10.10.2026 00:00 UTC — синтетическая запись\n\nТолько fixture: NOT RUN, без внешних effects.\n';
const revision = 'a'.repeat(40);
function applyRollFixture(root, previous, archived = Buffer.alloc(0)) {
  const planned = buildRollingArtifacts(root, previous, revision, archived);
  for (const [file, bytes] of planned) fs.writeFileSync(path.join(root, file), bytes);
  return planned;
}
test('Rolling сохраняет точные checkpoint bytes, checkbox preimage и frozen original5 parts', () => {
  withFixture(root => {
    const text = baselineText.replace('- [ ] 11.', '- [x] 11.');
    const previous = Buffer.from(text + ACTIVE_DELIMITER + checkpoint + checkpoint.replace('00:00', '00:01'));
    fs.writeFileSync(path.join(root, JOURNAL), previous);
    const planned = applyRollFixture(root, previous);
    assert.equal(planned.size, 4);
    const next = fs.readFileSync(path.join(root, JOURNAL));
    assert.equal(next.toString('utf8').split(ACTIVE_DELIMITER)[0], text);
    const rolling = readRolling(root, baseline, next);
    assert.deepEqual(rolling.history, Buffer.from(checkpoint + checkpoint.replace('00:00', '00:01')));
    assert.equal(rolling.parts[0].source.gitBlob, blobSha(previous));
    assert.deepEqual(fs.readFileSync(path.join(root, ARCHIVE, rolling.parts[0].source.file)), previous);
    for (const [file, bytes] of artifacts) if (file !== JOURNAL) assert.deepEqual(fs.readFileSync(path.join(root, file)), bytes);
    assert.doesNotThrow(() => verifyArtifacts(root, previous));
    fs.appendFileSync(path.join(root, JOURNAL), checkpoint.replace('00:00', '00:02'));
    assert.doesNotThrow(() => verifyArtifacts(root, previous));
  });
});
test('Повторный rolling восстанавливает полный предыдущий prefix и не переносит headings внутри fenced текста', () => {
  withFixture(root => {
    const first = Buffer.from(baselineText + ACTIVE_DELIMITER + checkpoint + checkpoint.replace('00:00', '00:01'));
    fs.writeFileSync(path.join(root, JOURNAL), first);
    applyRollFixture(root, first);
    const oldArchived = readRolling(root, baseline, fs.readFileSync(path.join(root, JOURNAL))).archived;
    const third = checkpoint.replace('00:00', '00:02') + '```text\n## Checkpoint внутри code block\n```\n';
    fs.appendFileSync(path.join(root, JOURNAL), third);
    const second = fs.readFileSync(path.join(root, JOURNAL));
    const planned = applyRollFixture(root, second, oldArchived);
    assert.ok(planned.get(JOURNAL).toString('utf8').endsWith(third));
    assert.doesNotThrow(() => verifyArtifacts(root, second, oldArchived));
    assert.doesNotThrow(() => verifyArtifacts(root, first));
    assert.equal(readRolling(root, baseline, planned.get(JOURNAL)).parts.length, 2);
    assert.throws(() => verifyArtifacts(root, second, Buffer.from('corrupt previous')), /CHECKPOINT_HISTORY_REWRITTEN/u);
  });
});
test('Rolling закрыто отвергает dropped/reordered/corrupt tail, previous source и pins', () => {
  for (const mutate of [
    (root, value) => fs.unlinkSync(path.join(root, ARCHIVE, value.parts[0].file)),
    (root, value) => fs.appendFileSync(path.join(root, ARCHIVE, value.parts[0].file), '\n'),
    (root, value) => { value.parts.reverse(); fs.writeFileSync(path.join(root, ROLLING_MANIFEST), JSON.stringify(value, null, 2) + '\n'); },
    (root, value) => { value.parts[0].bytes[0] = 1; fs.writeFileSync(path.join(root, ROLLING_MANIFEST), JSON.stringify(value, null, 2) + '\n'); },
    (root, value) => fs.appendFileSync(path.join(root, ARCHIVE, value.parts[0].source.file), '\n'),
    (root, value) => { value.parts[0].source.gitBlob = 'b'.repeat(40); fs.writeFileSync(path.join(root, ROLLING_MANIFEST), JSON.stringify(value, null, 2) + '\n'); },
    (root, value) => { value.baselineSha256 = 'b'.repeat(64); fs.writeFileSync(path.join(root, ROLLING_MANIFEST), JSON.stringify(value, null, 2) + '\n'); },
    (root, value) => { value.parts[0].file = '../other.txt'; fs.writeFileSync(path.join(root, ROLLING_MANIFEST), JSON.stringify(value, null, 2) + '\n'); },
  ]) withFixture(root => {
    let previous = Buffer.from(baselineText + ACTIVE_DELIMITER + checkpoint + checkpoint.replace('00:00', '00:01'));
    fs.writeFileSync(path.join(root, JOURNAL), previous);
    applyRollFixture(root, previous);
    const old = readRolling(root, baseline, fs.readFileSync(path.join(root, JOURNAL))).archived;
    fs.appendFileSync(path.join(root, JOURNAL), checkpoint.replace('00:00', '00:02'));
    previous = fs.readFileSync(path.join(root, JOURNAL));
    applyRollFixture(root, previous, old);
    mutate(root, JSON.parse(fs.readFileSync(path.join(root, ROLLING_MANIFEST))));
    assert.throws(() => verifyArtifacts(root, previous, old));
  });
});
test('Rolling не принимает изменённый preimage, malformed checkpoint и неподтверждённую revision', () => {
  withFixture(root => {
    const previous = Buffer.from(baselineText + ACTIVE_DELIMITER + checkpoint + checkpoint.replace('00:00', '00:01'));
    fs.writeFileSync(path.join(root, JOURNAL), previous);
    assert.throws(() => buildRollingArtifacts(root, previous, 'HEAD'), /ROLLING_REVISION_INVALID/u);
    fs.appendFileSync(path.join(root, JOURNAL), checkpoint.replace('00:00', '00:02'));
    assert.throws(() => buildRollingArtifacts(root, previous, revision), /ROLLING_PREIMAGE_MISMATCH/u);
    fs.writeFileSync(path.join(root, JOURNAL), baselineText + ACTIVE_DELIMITER + checkpoint);
    assert.throws(() => buildRollingArtifacts(root, fs.readFileSync(path.join(root, JOURNAL)), revision), /ROLLING_NOTHING_TO_ARCHIVE/u);
    fs.appendFileSync(path.join(root, JOURNAL), '\n## Checkpoint invalid\n');
    assert.throws(() => buildRollingArtifacts(root, fs.readFileSync(path.join(root, JOURNAL)), revision));
  });
});
test('Только текущие canonical checklist1–16 и подпункты допускают [ ]↔[x]', () => {
  withFixture(root => {
    const text = baseline.toString('utf8');
    const start = text.indexOf('## Обязательный checklist\n\n');
    const end = text.indexOf('## Карта новых пользовательских сценариев\n', start);
    const flipped = text.slice(0, start) + text.slice(start, end).replace(/^(\s*- \[)([ x])(\])/gmu, (_, a, b, c) => a + (b === 'x' ? ' ' : 'x') + c) + text.slice(end) + ACTIVE_DELIMITER;
    fs.writeFileSync(path.join(root, JOURNAL), flipped);
    assert.doesNotThrow(() => verifyArtifacts(root));
    assert.ok(flipped.includes('- [x] 11.'));
    assert.ok(flipped.includes('- [ ] 6.1.'));
    assert.ok(flipped.includes('  - [x] В существующем чате'));
  });
});
test('Append после delimiter принимается в128KiB без изменения baseline и manifest', () => {
  withFixture(root => {
    fs.appendFileSync(path.join(root, JOURNAL), checkpoint);
    assert.doesNotThrow(() => verifyArtifacts(root));
    assert.equal(sha256(fs.readFileSync(path.join(root, ARCHIVE, 'manifest.json'))), sha256(artifacts.get(`${ARCHIVE}/manifest.json`)));
  });
});
test('Byte-exact128KiB и корректная ссылка текущего checkpoint разрешены', () => {
  withFixture(root => {
    const text = baseline.toString('utf8') + ACTIVE_DELIMITER + checkpoint + '[каноника](../../AGENTS.md)\n';
    const bounded = text + 'a'.repeat(ACTIVE_LIMIT - Buffer.byteLength(text) - 1) + '\n';
    fs.writeFileSync(path.join(root, JOURNAL), bounded);
    assert.equal(Buffer.byteLength(bounded), ACTIVE_LIMIT);
    assert.doesNotThrow(() => verifyArtifacts(root));
    fs.appendFileSync(path.join(root, JOURNAL), '\n');
    assert.throws(() => verifyArtifacts(root), /ACTIVE_SIZE_INVALID/u);
  });
});
test('Исторический checkbox внутри byte-exact архива не становится mutable', () => {
  withFixture(root => {
    const file = path.join(root, ARCHIVE, manifest.parts[0].file);
    const text = fs.readFileSync(file, 'utf8');
    const flipped = text.replace(/\[([ x])\]/u, (_, b) => '[' + (b === 'x' ? ' ' : 'x') + ']');
    assert.notEqual(text, flipped);
    fs.writeFileSync(file, flipped);
    assert.throws(() => verifyArtifacts(root), /PART_IDENTITY_MISMATCH/u);
  });
});
test('Предыдущий snapshot доказывает append-only и отвергает изменение/удаление существующей записи', () => {
  withFixture(root => {
    const previous = Buffer.concat([baseline, Buffer.from(ACTIVE_DELIMITER + checkpoint)]);
    const next = checkpoint.replace('00:00', '00:01');
    fs.writeFileSync(path.join(root, JOURNAL), Buffer.concat([previous, Buffer.from(next)]));
    assert.doesNotThrow(() => verifyArtifacts(root, previous));
    fs.writeFileSync(path.join(root, JOURNAL), baseline.toString('utf8') + ACTIVE_DELIMITER + checkpoint.replace('NOT RUN', 'PASS'));
    assert.throws(() => verifyArtifacts(root, previous), /CHECKPOINT_HISTORY_REWRITTEN/u);
    fs.writeFileSync(path.join(root, JOURNAL), baseline.toString('utf8') + ACTIVE_DELIMITER);
    assert.throws(() => verifyArtifacts(root, previous), /CHECKPOINT_HISTORY_REWRITTEN/u);
  });
});
test('Нормативный текст, обычный текст и исторические статусы baseline не изменяются', () => {
  for (const [before, after] of [
    ['не более пяти циклов', 'не более шести циклов'],
    ['полное QA-задание', 'частичное QA-задание'],
    ['Diff-check PASS; ещё не main', 'Diff-check PASS; уже main'],
    ['Реализация и её проверки пока NOT RUN.', 'Реализация и её проверки пока PASS.'],
  ]) withFixture(root => {
    assert.ok(active.includes(before));
    fs.writeFileSync(path.join(root, JOURNAL), active.replace(before, after));
    assert.throws(() => verifyArtifacts(root), /ACTIVE_BASELINE_MISMATCH/u);
  });
});
test('Oversize, malformed append, invalidUTF8/NUL, второй DocID, неверные ссылки закрыто отклоняются', () => {
  for (const suffix of [
    'Текст без checkpoint\n',
    '\n## Checkpoint 10.10.2026 00:00 UTC — пустая запись\n',
    checkpoint.replace('10.10.2026', '31.02.2026'),
    checkpoint + '```\nunclosed\n',
    checkpoint + 'id: OPS-DOC-SELFDEV-001\n',
    checkpoint + '\x00\n',
    checkpoint + '[нет](missing-file.md)\n',
    checkpoint + '[нет](../../AGENTS.md#missing-anchor)\n',
    checkpoint + '[незакрытая](missing-file.md\n',
    checkpoint + ACTIVE_DELIMITER,
  ]) withFixture(root => {
    fs.appendFileSync(path.join(root, JOURNAL), suffix);
    assert.throws(() => verifyArtifacts(root));
  });
  withFixture(root => {
    fs.appendFileSync(path.join(root, JOURNAL), Buffer.from([0xff, 10]));
    assert.throws(() => verifyArtifacts(root));
  });
  withFixture(root => {
    const text = baseline.toString('utf8') + ACTIVE_DELIMITER + checkpoint;
    fs.writeFileSync(path.join(root, JOURNAL), text + 'я'.repeat(ACTIVE_LIMIT));
    assert.throws(() => verifyArtifacts(root), /ACTIVE_SIZE_INVALID/u);
  });
});
