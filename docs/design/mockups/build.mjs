import { writeFileSync } from 'node:fs';

const writeGenerated = (relativePath, content) => {
  const normalized = content.replace(/[ \t]+$/gm, '');
  writeFileSync(new URL(relativePath, import.meta.url), normalized);
};

const D = Object.assign({}, ...await Promise.all([
  import('./_d01.mjs'), import('./_d02.mjs'), import('./_d03.mjs'), import('./_d04.mjs'),
  import('./_d05.mjs'), import('./_d06.mjs'), import('./_d07.mjs'), import('./_d08.mjs'),
  import('./_d09.mjs'), import('./_d10.mjs'), import('./_d11.mjs'), import('./_d13.mjs'),
  import('./_d14.mjs'), import('./_d15.mjs'), import('./_d16.mjs'), import('./_d17.mjs'),
  import('./_d18.mjs'), import('./_d19.mjs'),
]));
const M = Object.assign({}, ...await Promise.all([
  import('./_m01.mjs'), import('./_m03.mjs'), import('./_m05.mjs'), import('./_m07.mjs'),
  import('./_m09.mjs'), import('./_m11.mjs'), import('./_m13.mjs'), import('./_m15.mjs'),
  import('./_m17.mjs'), import('./_m19.mjs'),
]));

// UX-12 desktop собран вручную (эталон дизайн-системы) и не перегенерируется.
export const SCREENS = [
  ['01', 'onboarding', 'Первичная настройка', '/onboarding', 'global', D.d01, M.m01],
  ['02', 'assistant', 'Помощник MatterCodex', '/assistant', 'global + Проект', D.d02, M.m02],
  ['03', 'home', 'Главная', '/', 'global', D.d03, M.m03],
  ['04', 'projects', 'Проекты', '/projects', 'global', D.d04, M.m04],
  ['05', 'project_overview', 'Обзор Проекта', '/projects/:projectRef', 'project', D.d05, M.m05],
  ['06', 'agents', 'ИИ-сотрудники', '/projects/:projectRef/agents', 'project', D.d06, M.m06],
  ['07', 'agent_detail', 'ИИ-сотрудник', '/projects/:projectRef/agents/:agentRef', 'project', D.d07, M.m07],
  ['08', 'workflows', 'Процессы', '/projects/:projectRef/workflows', 'project', D.d08, M.m08],
  ['09', 'workflow_detail', 'Редактор Процесса', '/projects/:projectRef/workflows/:workflowRef', 'project', D.d09, M.m09],
  ['10', 'new_run', 'Новый запуск', '/projects/:projectRef/runs/new', 'project', D.d10, M.m10],
  ['11', 'runs', 'Запуски', '/runs · /projects/:projectRef/runs', 'global / project', D.d11, M.m11],
  ['12', 'live_run', 'Live Run Workspace', '/runs/:runRef', 'global + Проект', null, M.m12],
  ['13', 'files_knowledge', 'Файлы и знания', '/projects/:projectRef/files', 'project', D.d13, M.m13],
  ['14', 'automations', 'Автоматизации', '/projects/:projectRef/automations', 'project', D.d14, M.m14],
  ['15', 'integrations', 'Интеграции', '/integrations', 'global', D.d15, M.m15],
  ['16', 'decisions', 'Решения', '/decisions', 'global', D.d16, M.m16],
  ['17', 'access', 'Участники и доступ', '/administration/access · /projects/:projectRef/members', 'global / project', D.d17, M.m17],
  ['18', 'administration', 'Администрирование', '/administration', 'global', D.d18, M.m18],
  ['19', 'audit_diagnostics', 'Аудит и диагностика', '/administration/audit', 'global', D.d19, M.m19],
];

let n = 0;
for (const [id, slug, , , , desk, mob] of SCREENS) {
  if (desk) { writeGenerated(`./${id}_${slug}_desktop.dc.html`, desk); n++; }
  writeGenerated(`./${id}_${slug}_mobile.dc.html`, mob); n++;
}
console.log('записано файлов экранов:', n);

// ---------- обложка ----------
const { cover } = await import('./_d00.mjs');
writeGenerated('./Main.dc.html', cover(SCREENS));

// ---------- раскладка холста ----------
const ROW = 1224, MOB_X = 1560;
const artboards = [
  { file: 'Main.dc.html', page: 'page-1', title: 'Карта макетов', x: 0, y: 0, w: 1440, h: 1024, print: 'fixed' },
];
SCREENS.forEach(([id, slug, name], i) => {
  const y = ROW + i * ROW;
  if (SCREENS[i][5]) {
    artboards.push({ file: `${id}_${slug}_desktop.dc.html`, page: 'page-1', title: `UX-${id} · ${name} · desktop`, x: 0, y, w: 1440, h: 1024, print: 'fixed' });
  } else {
    artboards.push({ file: `${id}_${slug}_desktop.dc.html`, page: 'page-1', title: `UX-${id} · ${name} · desktop`, x: 0, y, w: 1440, h: 1024, print: 'fixed' });
  }
  artboards.push({ file: `${id}_${slug}_mobile.dc.html`, page: 'page-1', title: `UX-${id} · ${name} · mobile`, x: MOB_X, y, w: 390, h: 844, print: 'fixed' });
});
['VariantAdark.dc.html', 'VariantB.dc.html', 'VariantC.dc.html', 'VariantD.dc.html'].forEach((f, i) => {
  const titles = ['Вариант А · тёмный', 'Вариант Б · Рабочее место', 'Вариант В · Отчёт', 'Вариант Г · Сетка'];
  artboards.push({ file: f, page: 'page-2', title: titles[i], x: i * 1560, y: 0, w: 1440, h: 1024, print: 'fixed' });
});

const groups = [
  ['grp-start', 0, 'НАЧАЛО РАБОТЫ\nUX-01 Первичная настройка, UX-02 Помощник MatterCodex, UX-03 Главная.\nЗдесь пользователь впервые видит платформу: помощник готов, интеграции не нужны, основной путь — «Начать с помощником».'],
  ['grp-projects', 3, 'ПРОЕКТЫ И КОМАНДА\nUX-04 Проекты, UX-05 Обзор Проекта, UX-06 ИИ-сотрудники, UX-07 ИИ-сотрудник.\nПроект — единственный контейнер работы. У агента версионируемые инструкции: опубликованная неизменяема, правки идут через черновик.'],
  ['grp-runs', 7, 'ПРОЦЕССЫ И ЗАПУСКИ\nUX-08 Процессы, UX-09 Редактор Процесса, UX-10 Новый запуск, UX-11 Запуски, UX-12 Live Run Workspace.\nСостояние определения Процесса нигде не смешивается с состоянием запуска. UX-12 — эталон дизайн-системы, с него начинался вариант А.'],
  ['grp-material', 12, 'МАТЕРИАЛЫ И ПОДКЛЮЧЕНИЯ\nUX-13 Файлы и знания, UX-14 Автоматизации, UX-15 Интеграции.\nПроверка безопасности файла показана отдельным этапом. Пустой каталог интеграций — валидное готовое состояние, а не ошибка.'],
  ['grp-admin', 15, 'РЕШЕНИЯ И АДМИНИСТРИРОВАНИЕ\nUX-16 Решения, UX-17 Участники и доступ, UX-18 Администрирование, UX-19 Аудит и диагностика.\nРешение человека принимается ровно один раз. Готовность ядра отделена от необязательных адаптеров, аудит показывает двойную атрибуцию «пользователь через помощника».'],
];
const annotations = groups.map(([id, i, text]) => ({ id, page: 'page-1', x: -700, y: ROW + i * ROW, w: 560, text }));
annotations.push({
  id: 'drafts', page: 'page-2', x: 0, y: -300, w: 620,
  text: 'ЧЕРНОВИКИ НАПРАВЛЕНИЙ\nЧетыре исходных направления экрана Live Run Workspace, из которых выбран вариант А (в светлой палитре).\nОставлены для истории решения; в производство идёт только страница «Макеты экранов».',
});

writeGenerated('./canvas.json', JSON.stringify({
  pages: [{ id: 'page-1', name: 'Макеты экранов' }, { id: 'page-2', name: 'Черновики направлений' }],
  artboards, annotations, launch: { view: 'canvas', page: 'page-1' },
}, null, 2) + '\n');

console.log('артбордов в canvas.json:', artboards.length);
console.log(artboards.map((a) => `--artboard ${a.file}`).join(' \\\n  '));

// ---------- index.md ----------
const rows = SCREENS.map(([id, slug, name, route, scope]) =>
  `| \`UX-${id}\` | ${name} | \`${route}\` | ${scope} | \`${id}_${slug}_desktop.dc.html\` | \`${id}_${slug}_mobile.dc.html\` |`).join('\n');

const md = `---
id: UX-MC-003
title: Индекс макетов web-first MatterCodex
type: product-design
status: approved
owner: product
version: 1.1.0
updated: 2026-08-23
---

# Индекс макетов web-first MatterCodex

Макеты к пакету промптов [UX-MC-002](../web-first-reset-prompt-pack.md).
19 экранов в двух размерах: **desktop 1440×1024** и **mobile 390×844**.
Один экран — один HTML-файл.

Утверждённые исходные canvas-экспорты владельца хранятся в репозитории:

- [полный набор экранов](../matter-codex-mockups.html);
- [детальный Live Run Workspace](../matter-codex-live-run.html).

Они являются binding UX source наравне с реестром ниже. Это HTML, а не PNG:
экспорт содержит самостоятельный интерактивный canvas и вложенные artboards.
Нормативные для реализации отдельные \`*.dc.html\` находятся в этом каталоге и
могут открываться локально без доступа к внешнему сервису.

Интерактивный холст со всеми макетами: <https://claude.ai/code/artifact/ab862d09-84c9-4501-be1e-dd936e9feda5>
(страница «Макеты экранов» — ряд на экран, слева desktop, справа mobile;
страница «Черновики направлений» — четыре исходных направления, из которых выбрано текущее).

## Именование

\`\`\`
NN_slug_desktop.dc.html    десктопный макет экрана UX-NN
NN_slug_mobile.dc.html     мобильный макет того же экрана
\`\`\`

\`NN\` совпадает с номером экрана в реестре UX-MC-002, \`slug\` — с именем из того же реестра.

## Реестр макетов

| ID | Экран | Route | Scope | Desktop | Mobile |
|---|---|---|---|---|---|
${rows}

Дополнительно: \`Main.dc.html\` — обложка холста с картой макетов, палитрой,
типографикой и перечнем состояний.

## Дизайн-система

Светлая тема, холодная нейтраль, один акцент.

| Роль | Значение |
|---|---|
| Акцент (действие, активное состояние) | \`#1B6FC4\`, тёмный \`#14589B\`, подложка \`#E8F1FB\` |
| Успех (готов, завершён, в сети) | \`#1A7A3C\`, подложка \`#E7F4EC\` |
| Внимание (решение человека, ожидание) | \`#8A6410\`, подложка \`#FDF8EC\` |
| Ошибка (сбой, изоляция, отмена) | \`#A32E2E\`, подложка \`#FCEDED\` |
| Текст | основной \`#10161E\`, вторичный \`#4E5A68\`, приглушённый \`#7C8794\` |
| Поверхности | белая \`#FFFFFF\`, панельная \`#FBFCFD\`, полотно графа \`#F7F9FB\`, поле \`#F3F6F9\` |
| Границы | \`#DFE4EA\`, усиленная \`#C9D1DA\`, волосяная \`#EFF2F5\` |
| Шрифты | IBM Plex Sans 400/500/600 + IBM Plex Mono для чисел и времени |
| Радиусы | 6 / 8 / 10 / 12 px |
| Высота контролов | desktop 32 px (мелкие 26 px), mobile 44–48 px |

Правила, соблюдённые во всех макетах:

- статус передаётся текстом и иконкой, не только цветом;
- кнопки «Обновить» нет ни на одном экране — данные приходят через realtime;
- не показываются UUID, digests, provider ID, локаторы Kubernetes, идентификаторы
  Mattermost, секреты и сырые ответы провайдеров;
- недоступная необязательная интеграция показана как отдельное ограничение и
  никогда не переводит основной результат в состояние ошибки;
- на mobile таблицы превращены в списки, боковые панели — в секции и нижние
  панели действий, зоны касания не меньше 44×44;
- на mobile не рисуется фальшивая системная строка iOS и фальшивая клавиатура.

## Исходники и сборка

Файлы \`*.dc.html\` — генерируемые. Источник:

| Файл | Что содержит |
|---|---|
| \`_lib.mjs\` | токены, иконки, атомы и оболочки (desktop shell, mobile shell) |
| \`_d00.mjs\` | обложка холста |
| \`_dNN.mjs\` | содержимое desktop-экранов |
| \`_mNN.mjs\` | содержимое mobile-экранов |
| \`build.mjs\` | сборка всех \`*.dc.html\`, \`canvas.json\` и этого файла |

\`\`\`bash
cd docs/design/mockups && node build.mjs
\`\`\`

Исключение: \`12_live_run_desktop.dc.html\` написан вручную — это эталон, с которого
началась дизайн-система, он не перегенерируется из \`build.mjs\`.

## Состояния вне основных artboards

UX-MC-002 перечисляет около тридцати отдельных состояний (loading, empty, error,
forbidden, offline, conflict, ongoing operation) — например
\`01_onboarding_bootstrap_error\`, \`12_live_run_gate_open\`,
\`16_decision_stale_winner\`. Здесь показано основное ready-состояние каждого
экрана. Полный контракт этих состояний обязателен для production Control Center
независимо от наличия отдельного статического artboard; отсутствие отдельного
HTML не означает перенос состояния за пределы reset.
`;
writeGenerated('./index.md', md);
console.log('index.md записан');
