// Дизайн-система макетов Kodex — светлый вариант А.
// Все размеры и цвета зафиксированы здесь; экраны собираются из этих примитивов.

export const T = {
  bg: '#FFFFFF', subtle: '#FBFCFD', canvas: '#F7F9FB', field: '#F3F6F9',
  line: '#DFE4EA', lineStrong: '#C9D1DA', hair: '#E6EBF0', row: '#EFF2F5',
  ink: '#10161E', ink2: '#24303D', sec: '#4E5A68', mut: '#7C8794', faint: '#8A94A0',
  acc: '#1B6FC4', accDark: '#14589B', accTint: '#E8F1FB', accLine: '#B9D6F2',
  accSoft: '#EFF6FD', accSoftLine: '#CFE1F5', accBar: '#DCE7F2',
  ok: '#1A7A3C', okTint: '#E7F4EC', okLine: '#B7DFC5', okSoft: '#F4FBF6', okAv: '#D7EEDF', okAvInk: '#14663A',
  warn: '#8A6410', warnTint: '#FDF8EC', warnLine: '#E8D3A0',
  err: '#A32E2E', errTint: '#FCEDED', errLine: '#E3C3C3',
  avBg: '#E3E9F0', avInk: '#34445A', accAv: '#DCEBF9',
  mono: "'IBM Plex Mono', monospace",
};

export const FONTS = 'https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500;600&display=swap';

export function page(bodyHtml) {
  return `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <script src="./support.js"></script>
</head>
<body>
<x-dc>
<helmet>
  <link rel="stylesheet" href="${FONTS}">
  <style>
    body { margin: 0; }
    a { color: ${T.acc}; text-decoration: none; }
    a:hover { color: #0F4E8F; }
  </style>
</helmet>
${bodyHtml}
</x-dc>
</body>
</html>
`;
}

export const frameDesktop = (inner) =>
  `<div style="width: 1440px; height: 1024px; display: flex; overflow: hidden; background: ${T.bg}; color: ${T.ink}; font-family: 'IBM Plex Sans', system-ui, sans-serif; font-size: 13px; line-height: 1.45; font-feature-settings: 'tnum' 1;">${inner}</div>`;

export const frameMobile = (inner) =>
  `<div style="width: 390px; height: 844px; display: flex; flex-direction: column; overflow: hidden; background: ${T.bg}; color: ${T.ink}; font-family: 'IBM Plex Sans', system-ui, sans-serif; font-size: 14px; line-height: 1.5; font-feature-settings: 'tnum' 1;">${inner}</div>`;

// ---------- иконки ----------
const ic = (d, sw = 1.7) =>
  `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round">${d}</svg>`;
export const I = {
  home: ic('<path d="M3 10.5 12 4l9 6.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1Z"/>'),
  folder: ic('<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>'),
  run: ic('<path d="M6 4.5v15l12-7.5Z"/>'),
  decision: ic('<path d="M9 12.5 11 15l4.5-5.5"/><circle cx="12" cy="12" r="8.5"/>'),
  plug: ic('<path d="M10 13a4 4 0 0 0 5.7.4l2.6-2.6a4 4 0 0 0-5.7-5.7l-1.3 1.3"/><path d="M14 11a4 4 0 0 0-5.7-.4l-2.6 2.6a4 4 0 0 0 5.7 5.7l1.3-1.3"/>'),
  gear: ic('<circle cx="12" cy="12" r="3"/><path d="M4.5 12a7.5 7.5 0 0 1 .3-2l-1.6-1.2 1.8-3.1 1.9.7A7.5 7.5 0 0 1 9.6 5l.3-2h3.6l.3 2c.7.2 1.4.6 2 1l1.9-.7 1.8 3.1L18 9.6c.1.4.2.8.2 1.2"/>'),
  bot: ic('<path d="M12 3a4 4 0 0 1 4 4v1h1a3 3 0 0 1 3 3v3a5 5 0 0 1-5 5H9a5 5 0 0 1-5-5v-3a3 3 0 0 1 3-3h1V7a4 4 0 0 1 4-4Z"/><path d="M9.5 13h.01M14.5 13h.01"/>'),
  search: ic('<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/>', 1.8),
  bell: ic('<path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6Z"/><path d="M10 20a2.4 2.4 0 0 0 4 0"/>'),
  check: ic('<path d="M5 12.5 10 17.5 19 7"/>', 2.4),
  spin: ic('<path d="M12 3.5a8.5 8.5 0 1 0 8.5 8.5"/>', 2.4),
  clock: ic('<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>', 2),
  shield: ic('<path d="M12 4.5 20 8v5c0 4-3.4 6-8 7-4.6-1-8-3-8-7V8Z"/>'),
  alert: ic('<path d="M12 8v5"/><path d="M12 16.5h.01"/><circle cx="12" cy="12" r="8.5"/>', 2),
  file: ic('<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z"/><path d="M14 3v5h5"/>'),
  plus: ic('<path d="M12 5v14M5 12h14"/>', 2),
  minus: ic('<path d="M5 12h14"/>', 2),
  chev: ic('<path d="m6 9 6 6 6-6"/>', 2),
  chevR: ic('<path d="m9 6 6 6-6 6"/>', 2),
  back: ic('<path d="M15 6l-6 6 6 6"/>', 2),
  menu: ic('<path d="M4 7h16M4 12h16M4 17h16"/>', 2),
  dots: '<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/></svg>',
  list: ic('<path d="M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01"/>', 1.8),
  grid: ic('<path d="M4 4h7v7H4ZM13 4h7v7h-7ZM4 13h7v7H4ZM13 13h7v7h-7Z"/>'),
  filter: ic('<path d="M4 6h16l-6 7v5l-4 2v-7Z"/>'),
  upload: ic('<path d="M12 17V6"/><path d="m7.5 10.5 4.5-4.5 4.5 4.5"/><path d="M5 19h14"/>', 1.8),
  users: ic('<circle cx="9" cy="9" r="3.2"/><path d="M3.5 19a5.5 5.5 0 0 1 11 0"/><path d="M16 6.2a3.2 3.2 0 0 1 0 5.6"/><path d="M17.5 14.4a5.5 5.5 0 0 1 3 4.6"/>'),
  calendar: ic('<rect x="4" y="5.5" width="16" height="14.5" rx="2"/><path d="M4 10h16M9 3.5v4M15 3.5v4"/>'),
  book: ic('<path d="M5 5.5A2 2 0 0 1 7 4h12v14H7a2 2 0 0 0-2 2Z"/><path d="M5 5.5V20"/>'),
  audit: ic('<path d="M6 3.5h9l4 4V20a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4.5a1 1 0 0 1 1-1Z"/><path d="M9 12h7M9 16h5"/>'),
  wf: ic('<rect x="3.5" y="4" width="6" height="5" rx="1.5"/><rect x="14.5" y="4" width="6" height="5" rx="1.5"/><rect x="9" y="15" width="6" height="5" rx="1.5"/><path d="M6.5 9v3h11V9M12 12v3"/>'),
};
const icSz = (svg, n) => svg.replace('width="16" height="16"', `width="${n}" height="${n}"`);
export const icon = (name, size = 16) => icSz(I[name], size);

// ---------- атомы ----------
export const statusPill = (kind, text, size = 'md') => {
  const map = {
    run: [T.accTint, T.accLine, T.accDark, 'spin'],
    done: [T.okTint, T.okLine, T.ok, 'check'],
    wait: [T.warnTint, T.warnLine, T.warn, 'clock'],
    gate: [T.warnTint, T.warnLine, T.warn, 'shield'],
    err: [T.errTint, T.errLine, T.err, 'alert'],
    off: [T.field, T.line, T.sec, 'minus'],
  };
  const [bg, bd, fg, i] = map[kind];
  const h = size === 'sm' ? 22 : 24, fs = size === 'sm' ? 11 : 11.5;
  return `<span style="display: inline-flex; align-items: center; gap: 6px; height: ${h}px; padding: 0 10px; border-radius: ${h / 2}px; background: ${bg}; border: 1px solid ${bd}; color: ${fg}; font-size: ${fs}px; font-weight: 500; white-space: nowrap;">${icon(i, 12)}${text}</span>`;
};

export const btn = (label, kind = 'sec', h = 32) => {
  const st = {
    pri: `border: 0; background: ${T.acc}; color: #FFFFFF; font-weight: 500;`,
    sec: `border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink};`,
    ghost: `border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec};`,
    danger: `border: 1px solid ${T.errLine}; background: ${T.bg}; color: ${T.err}; font-weight: 500;`,
    off: `border: 1px solid ${T.line}; background: ${T.field}; color: ${T.faint};`,
  }[kind];
  return `<button style="height: ${h}px; padding: 0 14px; border-radius: 8px; ${st} font: inherit; font-size: 12.5px;">${label}</button>`;
};
export const iconBtn = (name, label, h = 32) =>
  `<button aria-label="${label}" style="width: ${h}px; height: ${h}px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; display: inline-flex; align-items: center; justify-content: center;">${icon(name, 16)}</button>`;

export const chip = (text, active = false) =>
  `<span style="display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 11px; border-radius: 8px; border: 1px solid ${active ? T.accLine : T.line}; background: ${active ? T.accTint : T.bg}; color: ${active ? T.accDark : T.sec}; font-size: 12px;">${text}</span>`;

export const avatar = (txt, size = 28, tone = 'neutral') => {
  const [bg, fg] = tone === 'acc' ? [T.accAv, T.accDark] : tone === 'ok' ? [T.okAv, T.okAvInk] : [T.avBg, T.avInk];
  return `<span style="width: ${size}px; height: ${size}px; flex: 0 0 ${size}px; border-radius: ${size / 2}px; background: ${bg}; color: ${fg}; font-size: ${Math.round(size * 0.38)}px; font-weight: 700; display: flex; align-items: center; justify-content: center;">${txt}</span>`;
};

export const sectionHead = (text, right = '') =>
  `<div style="display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 0 0 10px;">
     <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">${text}</h2>${right}
   </div>`;

export const card = (inner, pad = 16, extra = '') =>
  `<div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: ${pad}px; ${extra}">${inner}</div>`;

export const metric = (value, label) =>
  `<div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 10px; padding: 12px 14px; background: ${T.bg};">
     <div style="font-size: 20px; font-weight: 600; letter-spacing: -0.02em;">${value}</div>
     <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${label}</div>
   </div>`;

export const field = (label, value, hint = '', w = '100%') =>
  `<label style="display: flex; flex-direction: column; gap: 6px; width: ${w};">
     <span style="font-size: 11.5px; color: ${T.sec};">${label}</span>
     <span style="min-height: 34px; display: flex; align-items: center; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; font-size: 13px; color: ${value ? T.ink : T.faint};">${value || hint}</span>
   </label>`;

export const textarea = (label, value, h = 90) =>
  `<label style="display: flex; flex-direction: column; gap: 6px;">
     <span style="font-size: 11.5px; color: ${T.sec};">${label}</span>
     <span style="min-height: ${h}px; display: block; padding: 10px 12px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; font-size: 13px; line-height: 1.55; color: ${T.ink};">${value}</span>
   </label>`;

export const monoTxt = (t, c = T.mut, s = 10.5) =>
  `<span style="font-family: ${T.mono}; font-size: ${s}px; color: ${c};">${t}</span>`;

export const searchBox = (ph, w = '100%') =>
  `<div style="flex: 0 1 ${w}; display: flex; align-items: center; gap: 8px; height: 32px; padding: 0 10px; border-radius: 8px; border: 1px solid ${T.hair}; background: ${T.field}; color: ${T.mut};">${icon('search', 14)}<span style="font-size: 12.5px;">${ph}</span></div>`;

// ---------- desktop shell ----------
const navItem = (name, label, active, badge) => `
  <a href="#" ${active ? 'aria-current="page"' : ''} style="display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 8px 12px; border-radius: 8px; ${active ? `background: ${T.accTint}; color: ${T.accDark}; box-shadow: inset 2px 0 0 ${T.acc};` : `color: ${T.sec};`}">
    <span style="display: flex; align-items: center; gap: 10px;"><span style="color: ${active ? T.acc : 'currentColor'}; display: flex;">${icon(name, 16)}</span><span style="${active ? 'font-weight: 600;' : ''}">${label}</span></span>
    ${badge ? `<span style="min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: ${T.warn}; color: #FFFFFF; font-size: 10.5px; font-weight: 600; display: flex; align-items: center; justify-content: center;">${badge}</span>` : ''}
  </a>`;

export function navDesktop(active, opts = {}) {
  const assistantActive = active === 'assistant';
  const st = opts.assistantStatus || ['Готов', T.ok];
  return `
  <nav style="width: 232px; flex: 0 0 232px; display: flex; flex-direction: column; background: ${T.subtle}; border-right: 1px solid ${T.line};">
    <div style="height: 56px; display: flex; align-items: center; gap: 10px; padding: 0 18px; border-bottom: 1px solid ${T.line};">
      <div style="width: 24px; height: 24px; border-radius: 6px; background: ${T.acc}; display: flex; align-items: center; justify-content: center;">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#FFFFFF" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M4 18V7l8 5 8-5v11"/></svg>
      </div>
      <span style="font-size: 14px; font-weight: 600; letter-spacing: -0.01em;">Kodex</span>
    </div>
    <div style="padding: 14px 12px 10px;">
      <a href="#" style="display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 8px; background: ${assistantActive ? T.accTint : '#F1F6FC'}; border: 1px solid ${assistantActive ? T.acc : T.accSoftLine}; color: ${T.ink};">
        <span style="width: 26px; height: 26px; flex: 0 0 26px; border-radius: 7px; background: ${T.bg}; border: 1px solid ${T.accSoftLine}; display: flex; align-items: center; justify-content: center; color: ${T.acc};">${icon('bot', 15)}</span>
        <span style="display: flex; flex-direction: column; gap: 1px;">
          <span style="font-size: 12.5px; font-weight: ${assistantActive ? 600 : 500};">Помощник Kodex</span>
          <span style="font-size: 10.5px; color: ${st[1]};">${st[0]}</span>
        </span>
      </a>
    </div>
    <div style="display: flex; flex-direction: column; gap: 2px; padding: 0 12px;">
      ${navItem('home', 'Главная', active === 'home')}
      ${navItem('folder', 'Проекты', active === 'projects')}
      ${navItem('run', 'Запуски', active === 'runs')}
      ${navItem('decision', 'Решения', active === 'decisions', opts.decisions ?? 1)}
      ${navItem('plug', 'Интеграции', active === 'integrations')}
    </div>
    <div style="flex: 1 1 auto;"></div>
    <div style="padding: 12px; border-top: 1px solid ${T.line};">
      ${navItem('gear', 'Администрирование', active === 'admin')}
    </div>
  </nav>`;
}

export function topbar(project = 'Все проекты', opts = {}) {
  const online = opts.offline
    ? `<span style="display: flex; align-items: center; gap: 7px; height: 26px; padding: 0 10px; border-radius: 13px; background: ${T.errTint}; border: 1px solid ${T.errLine}; color: ${T.err}; font-size: 11.5px;">${icon('alert', 12)}Нет соединения</span>`
    : `<span style="display: flex; align-items: center; gap: 7px; height: 26px; padding: 0 10px; border-radius: 13px; background: ${T.okTint}; border: 1px solid ${T.okLine}; color: ${T.ok}; font-size: 11.5px;">${icon('check', 12)}В сети</span>`;
  return `
  <header style="height: 56px; flex: 0 0 56px; display: flex; align-items: center; gap: 14px; padding: 0 20px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <button style="display: flex; align-items: center; gap: 8px; height: 32px; padding: 0 10px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">
      <span style="width: 6px; height: 6px; border-radius: 3px; background: ${T.acc};"></span>${project}
      <span style="color: ${T.mut}; display: flex;">${icon('chev', 13)}</span>
    </button>
    ${searchBox('Найти проект, агента или запуск', '360px')}
    <div style="flex: 1 1 auto;"></div>
    ${online}
    <span style="position: relative; display: flex; align-items: center; justify-content: center; width: 32px; height: 32px; border-radius: 8px; border: 1px solid ${T.line}; color: ${T.sec};">${icon('decision', 16)}
      <span style="position: absolute; top: -5px; right: -5px; min-width: 16px; height: 16px; padding: 0 4px; border-radius: 8px; background: ${T.warn}; color: #FFFFFF; font-size: 10px; font-weight: 700; display: flex; align-items: center; justify-content: center;">${opts.decisions ?? 1}</span>
    </span>
    <span style="display: flex; align-items: center; justify-content: center; width: 32px; height: 32px; border-radius: 8px; border: 1px solid ${T.line}; color: ${T.sec};">${icon('bell', 16)}</span>
    <span style="display: flex; align-items: center; gap: 8px; padding-left: 6px;">
      ${avatar('АВ', 28)}
      <span style="display: flex; flex-direction: column;">
        <span style="font-size: 12px;">Анна Волкова</span>
        <span style="font-size: 10.5px; color: ${T.mut};">${opts.role || 'Владелец'}</span>
      </span>
    </span>
  </header>`;
}

export function projectNav(active) {
  const items = [
    ['overview', 'Обзор'], ['agents', 'ИИ-сотрудники'], ['workflows', 'Процессы'],
    ['runs', 'Запуски'], ['files', 'Файлы и знания'], ['automations', 'Автоматизации'], ['members', 'Участники'],
  ];
  return `<div style="height: 44px; flex: 0 0 44px; display: flex; align-items: stretch; gap: 4px; padding: 0 24px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    ${items.map(([k, l]) => `<span style="display: flex; align-items: center; padding: 0 12px; font-size: 12.5px; ${k === active ? `color: ${T.ink}; font-weight: 600; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${l}</span>`).join('')}
  </div>`;
}

export const crumbs = (parts) =>
  `<div style="display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: ${T.mut};">${parts
    .map((p, i) => (i === parts.length - 1 ? `<span>${p}</span>` : `<a href="#">${p}</a><span>/</span>`))
    .join('')}</div>`;

export function contentHead({ path, title, sub, actions = '', pill = '' }) {
  return `<div style="padding: 16px 24px 14px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    ${crumbs(path)}
    <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-top: 8px;">
      <div style="min-width: 0;">
        <div style="display: flex; align-items: center; gap: 12px;">
          <h1 style="margin: 0; font-size: 19px; font-weight: 600; letter-spacing: -0.015em;">${title}</h1>${pill}
        </div>
        ${sub ? `<div style="font-size: 12.5px; color: ${T.sec}; margin-top: 5px;">${sub}</div>` : ''}
      </div>
      <div style="display: flex; align-items: center; gap: 8px; flex: 0 0 auto;">${actions}</div>
    </div>
  </div>`;
}

export const shellDesktop = ({ nav, project, navOpts = {}, body }) =>
  frameDesktop(`${navDesktop(nav, navOpts)}<div style="flex: 1 1 auto; display: flex; flex-direction: column; min-width: 0;">${topbar(project, navOpts)}${body}</div>`);

// ---------- mobile shell ----------
export function mTop({ title, back = false, sub = '', right = 'avatar' }) {
  const lead = back
    ? `<button aria-label="Назад" style="width: 44px; height: 44px; margin-left: -10px; border: 0; background: none; color: ${T.ink}; display: flex; align-items: center; justify-content: center;">${icon('back', 22)}</button>`
    : `<button aria-label="Меню" style="width: 44px; height: 44px; margin-left: -10px; border: 0; background: none; color: ${T.ink}; display: flex; align-items: center; justify-content: center;">${icon('menu', 22)}</button>`;
  const tail = right === 'none' ? '' : right === 'dots'
    ? `<button aria-label="Другие действия" style="width: 44px; height: 44px; margin-right: -10px; border: 0; background: none; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${icon('dots', 20)}</button>`
    : `<span style="display: flex; align-items: center; gap: 4px;"><button aria-label="Поиск" style="width: 44px; height: 44px; border: 0; background: none; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${icon('search', 20)}</button>${avatar('АВ', 30)}</span>`;
  return `<header style="flex: 0 0 ${sub ? 76 : 56}px; display: flex; flex-direction: column; justify-content: center; padding: 0 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <div style="display: flex; align-items: center; gap: 6px;">
      ${lead}
      <span style="flex: 1 1 auto; min-width: 0; font-size: 16px; font-weight: 600; letter-spacing: -0.01em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${title}</span>
      ${tail}
    </div>
    ${sub ? `<div style="font-size: 12px; color: ${T.mut}; padding: 2px 0 0 34px;">${sub}</div>` : ''}
  </header>`;
}

export const mScroll = (inner, pad = 14) =>
  `<div style="flex: 1 1 auto; overflow: hidden; padding: ${pad}px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle};">${inner}</div>`;

export const mCard = (inner, extra = '') =>
  `<div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 13px; ${extra}">${inner}</div>`;

export const mBtn = (label, kind = 'pri') => {
  const st = {
    pri: `border: 0; background: ${T.acc}; color: #FFFFFF; font-weight: 600;`,
    sec: `border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font-weight: 500;`,
    danger: `border: 1px solid ${T.errLine}; background: ${T.bg}; color: ${T.err}; font-weight: 500;`,
    off: `border: 1px solid ${T.line}; background: ${T.field}; color: ${T.faint};`,
  }[kind];
  return `<button style="width: 100%; height: 46px; border-radius: 10px; ${st} font: inherit; font-size: 15px;">${label}</button>`;
};

export const mSectionTitle = (t, right = '') =>
  `<div style="display: flex; align-items: baseline; justify-content: space-between; gap: 10px; padding-top: 2px;">
     <h2 style="margin: 0; font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">${t}</h2>${right}
   </div>`;

export const mBottom = (inner) =>
  `<div style="flex: 0 0 auto; padding: 10px 14px 14px; border-top: 1px solid ${T.line}; background: ${T.bg}; display: flex; flex-direction: column; gap: 8px;">${inner}</div>`;

export const mTabs = (items, active) =>
  `<div style="display: flex; gap: 18px; padding: 0 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg}; flex: 0 0 auto;">
     ${items.map((t) => `<span style="padding: 10px 0; font-size: 13.5px; ${t === active ? `font-weight: 600; color: ${T.ink}; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${t}</span>`).join('')}
   </div>`;
