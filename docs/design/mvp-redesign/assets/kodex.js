/* =====================================================================
   Kodex Control Center · MVP redesign · shell + behaviours
   Минимальный локальный JavaScript без зависимостей. Не production-код:
   макеты, поведение controls и синтетические данные.
   ===================================================================== */
(function (global) {
  'use strict';

  /* ------------------------------------------------------------------
     Иконки: один SVG-sprite, ссылки через <svg class="ic"><use href="#i-..."/>
  ------------------------------------------------------------------ */
  const ICONS = {
    home: '<path d="M3 10.5 12 4l9 6.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1Z"/>',
    folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
    run: '<path d="M6 4.5v15l12-7.5Z"/>',
    decision: '<path d="M9 12.5 11 15l4.5-5.5"/><circle cx="12" cy="12" r="8.5"/>',
    plug: '<path d="M10 13a4 4 0 0 0 5.7.4l2.6-2.6a4 4 0 0 0-5.7-5.7l-1.3 1.3"/><path d="M14 11a4 4 0 0 0-5.7-.4l-2.6 2.6a4 4 0 0 0 5.7 5.7l1.3-1.3"/>',
    gear: '<circle cx="12" cy="12" r="3"/><path d="M4.5 12a7.5 7.5 0 0 1 .3-2l-1.6-1.2 1.8-3.1 1.9.7A7.5 7.5 0 0 1 9.6 5l.3-2h3.6l.3 2c.7.2 1.4.6 2 1l1.9-.7 1.8 3.1L18 9.6c.1.4.2.8.2 1.2"/>',
    bot: '<path d="M12 3a4 4 0 0 1 4 4v1h1a3 3 0 0 1 3 3v3a5 5 0 0 1-5 5H9a5 5 0 0 1-5-5v-3a3 3 0 0 1 3-3h1V7a4 4 0 0 1 4-4Z"/><path d="M9.5 13h.01M14.5 13h.01"/>',
    search: '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/>',
    bell: '<path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6Z"/><path d="M10 20a2.4 2.4 0 0 0 4 0"/>',
    check: '<path d="M5 12.5 10 17.5 19 7"/>',
    spin: '<path d="M12 3.5a8.5 8.5 0 1 0 8.5 8.5"/>',
    clock: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
    shield: '<path d="M12 4.5 20 8v5c0 4-3.4 6-8 7-4.6-1-8-3-8-7V8Z"/>',
    'shield-check': '<path d="M12 4.5 20 8v5c0 4-3.4 6-8 7-4.6-1-8-3-8-7V8Z"/><path d="m9 12 2 2 4-4"/>',
    alert: '<path d="M12 8v5"/><path d="M12 16.5h.01"/><circle cx="12" cy="12" r="8.5"/>',
    warning: '<path d="M12 3.5 21 19H3Z"/><path d="M12 10v4"/><path d="M12 16.5h.01"/>',
    info: '<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5"/><path d="M12 7.5h.01"/>',
    file: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z"/><path d="M14 3v5h5"/>',
    files: '<path d="M8 3h7l4 4v9a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z"/><path d="M15 3v4h4"/><path d="M4 8v11a2 2 0 0 0 2 2h9"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    minus: '<path d="M5 12h14"/>',
    chev: '<path d="m6 9 6 6 6-6"/>',
    'chev-r': '<path d="m9 6 6 6-6 6"/>',
    'chev-l': '<path d="m15 6-6 6 6 6"/>',
    'chev-u': '<path d="m6 15 6-6 6 6"/>',
    back: '<path d="M15 6l-6 6 6 6"/>',
    'arrow-r': '<path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>',
    'arrow-l': '<path d="M19 12H5"/><path d="m11 6-6 6 6 6"/>',
    menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
    dots: '<circle cx="5" cy="12" r="1.2" fill="currentColor"/><circle cx="12" cy="12" r="1.2" fill="currentColor"/><circle cx="19" cy="12" r="1.2" fill="currentColor"/>',
    list: '<path d="M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01"/>',
    grid: '<path d="M4 4h7v7H4ZM13 4h7v7h-7ZM4 13h7v7H4ZM13 13h7v7h-7Z"/>',
    filter: '<path d="M4 6h16l-6 7v5l-4 2v-7Z"/>',
    upload: '<path d="M12 17V6"/><path d="m7.5 10.5 4.5-4.5 4.5 4.5"/><path d="M5 19h14"/>',
    download: '<path d="M12 5v11"/><path d="m7.5 12.5 4.5 4.5 4.5-4.5"/><path d="M5 20h14"/>',
    users: '<circle cx="9" cy="9" r="3.2"/><path d="M3.5 19a5.5 5.5 0 0 1 11 0"/><path d="M16 6.2a3.2 3.2 0 0 1 0 5.6"/><path d="M17.5 14.4a5.5 5.5 0 0 1 3 4.6"/>',
    user: '<circle cx="12" cy="8.5" r="3.5"/><path d="M5 20a7 7 0 0 1 14 0"/>',
    calendar: '<rect x="4" y="5.5" width="16" height="14.5" rx="2"/><path d="M4 10h16M9 3.5v4M15 3.5v4"/>',
    book: '<path d="M5 5.5A2 2 0 0 1 7 4h12v14H7a2 2 0 0 0-2 2Z"/><path d="M5 5.5V20"/>',
    audit: '<path d="M6 3.5h9l4 4V20a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4.5a1 1 0 0 1 1-1Z"/><path d="M9 12h7M9 16h5"/>',
    wf: '<rect x="3.5" y="4" width="6" height="5" rx="1.5"/><rect x="14.5" y="4" width="6" height="5" rx="1.5"/><rect x="9" y="15" width="6" height="5" rx="1.5"/><path d="M6.5 9v3h11V9M12 12v3"/>',
    x: '<path d="M6 6l12 12M18 6 6 18"/>',
    edit: '<path d="M4 20h4l10.5-10.5a2.1 2.1 0 0 0-3-3L5 17Z"/><path d="m13.5 6.5 3 3"/>',
    copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>',
    trash: '<path d="M4 7h16"/><path d="M10 11v6M14 11v6"/><path d="M6 7l1 13h10l1-13"/><path d="M9 7V4h6v3"/>',
    eye: '<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z"/><circle cx="12" cy="12" r="3"/>',
    'eye-off': '<path d="M3 3l18 18"/><path d="M10 6.2A9 9 0 0 1 12 6c6 0 9.5 6 9.5 6a16 16 0 0 1-3 3.6"/><path d="M6.6 6.6C4 8.4 2.5 12 2.5 12s3.5 6 9.5 6a9 9 0 0 0 4-1"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/>',
    refresh: '<path d="M20 12a8 8 0 1 1-2.3-5.7"/><path d="M20 4v5h-5"/>',
    link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7L11.5 6.8"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.5-1.5"/>',
    external: '<path d="M14 4h6v6"/><path d="M20 4 10 14"/><path d="M18 13v6a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h6"/>',
    'zoom-in': '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/><path d="M11 8v6M8 11h6"/>',
    'zoom-out': '<circle cx="11" cy="11" r="6.5"/><path d="m16 16 4 4"/><path d="M8 11h6"/>',
    fit: '<path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5"/>',
    monitor: '<rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M8 20h8M12 16.5V20"/>',
    automation: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/><path d="M3.5 12H6M18 12h2.5"/>',
    send: '<path d="M4 12 20 4l-4 16-4-7Z"/><path d="M12 13 20 4"/>',
    mic: '<rect x="9" y="3.5" width="6" height="11" rx="3"/><path d="M6 11a6 6 0 0 0 12 0"/><path d="M12 17v3.5M9 20.5h6"/>',
    paperclip: '<path d="m20 11-8.5 8.5a5 5 0 0 1-7-7L13 4a3.3 3.3 0 0 1 4.7 4.7L9.5 17a1.6 1.6 0 0 1-2.3-2.3L15 7"/>',
    terminal: '<rect x="3" y="4.5" width="18" height="15" rx="2"/><path d="m7 9 3 3-3 3M12 15h5"/>',
    code: '<path d="m8 8-4 4 4 4M16 8l4 4-4 4M14 5l-4 14"/>',
    tool: '<path d="M14.5 6.5a4 4 0 0 0 4.9 4.9L13 17.8a2.3 2.3 0 0 1-3.3-3.3l6.4-6.4a4 4 0 0 0-1.6-1.6Z"/><path d="m4 20 4-4"/>',
    branch: '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="9" r="2"/><path d="M6 7v10"/><path d="M18 11a6 6 0 0 1-6 6H6"/>',
    lock: '<rect x="5" y="10.5" width="14" height="10" rx="2"/><path d="M8 10.5V7.5a4 4 0 0 1 8 0v3"/>',
    key: '<circle cx="8" cy="14" r="4"/><path d="m11 11 8.5-8.5"/><path d="m16 6 2.5 2.5M18.5 3.5 21 6"/>',
    globe: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c3 3 3 14 0 17M12 3.5c-3 3-3 14 0 17"/>',
    mail: '<rect x="3" y="5.5" width="18" height="13" rx="2"/><path d="m3.5 7 8.5 6 8.5-6"/>',
    image: '<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="m4 18 5-5 3 3 3.5-3.5L20 17"/>',
    layers: '<path d="m12 4 8.5 4.5L12 13 3.5 8.5Z"/><path d="m3.5 12.5 8.5 4.5 8.5-4.5"/><path d="m3.5 16.5 8.5 4.5 8.5-4.5"/>',
    package: '<path d="m12 3 8.5 4.5v9L12 21l-8.5-4.5v-9Z"/><path d="M3.8 7.7 12 12l8.2-4.3M12 12v9"/>',
    pause: '<path d="M8 5v14M16 5v14"/>',
    play: '<path d="M7 4.5v15l12-7.5Z"/>',
    archive: '<rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9"/><path d="M10 13h4"/>',
    sort: '<path d="M7 4v16M7 20l-3-3M7 20l3-3M17 20V4M17 4l-3 3M17 4l3 3"/>',
    columns: '<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M9 4.5v15M15 4.5v15"/>',
    maximize: '<path d="M8 4H4v4M16 4h4v4M20 16v4h-4M4 16v4h4"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M5.6 18.4 7 17M17 7l1.4-1.4"/>',
    moon: '<path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z"/>',
    'x-circle': '<circle cx="12" cy="12" r="8.5"/><path d="m9 9 6 6M15 9l-6 6"/>',
    'check-circle': '<circle cx="12" cy="12" r="8.5"/><path d="m8.5 12.5 2.5 2.5 5-5"/>',
    history: '<path d="M4 12a8 8 0 1 0 2.3-5.7"/><path d="M4 4v5h5"/><path d="M12 8v4l3 2"/>',
    tag: '<path d="M4 4h7l9 9-7 7-9-9Z"/><path d="M8 8h.01"/>',
    building: '<rect x="4" y="3.5" width="16" height="17" rx="1.5"/><path d="M9 8h2M13 8h2M9 12h2M13 12h2M9 16h2M13 16h2"/>',
    ticket: '<path d="M3.5 9a2 2 0 0 0 0 6v3h17v-3a2 2 0 0 0 0-6V6h-17Z"/><path d="M12 6v12"/>',
    cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9.5" y="9.5" width="5" height="5"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/>',
    box: '<rect x="3.5" y="6.5" width="17" height="13" rx="2"/><path d="M3.5 10.5h17M8 6.5V4M16 6.5V4"/>',
    sparkle: '<path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2 2M16 16l2 2M6 18l2-2M16 8l2-2"/>',
    star: '<path d="m12 3.5 2.6 5.4 5.9.8-4.3 4.1 1.1 5.8L12 16.8l-5.3 2.8 1.1-5.8-4.3-4.1 5.9-.8Z"/>',
    flag: '<path d="M5 21V4"/><path d="M5 4h12l-2 4 2 4H5"/>',
    inbox: '<path d="M4 13h4l2 3h4l2-3h4"/><path d="M5 5.5h14l2 7.5v6H3v-6Z"/>',
    stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
    'file-text': '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z"/><path d="M14 3v5h5"/><path d="M9 13h6M9 17h6"/>',
    activity: '<path d="M3 12h4l2.5-6 4 12 2.5-6h5"/>',
    wifi: '<path d="M3 9.5a14 14 0 0 1 18 0"/><path d="M6.5 13a9 9 0 0 1 11 0"/><path d="M10 16.5a4 4 0 0 1 4 0"/><path d="M12 20h.01"/>',
    'wifi-off': '<path d="M3 3l18 18"/><path d="M3 9.5a14 14 0 0 1 6.3-3.3"/><path d="M14.5 6.2a14 14 0 0 1 6.5 3.3"/><path d="M6.5 13a9 9 0 0 1 4-2.2"/><path d="M14.8 11.5a9 9 0 0 1 2.7 1.5"/><path d="M10 16.5a4 4 0 0 1 4 0"/><path d="M12 20h.01"/>',
    graph: '<circle cx="5" cy="12" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="19" cy="18" r="2"/><path d="M7 11.5 17 7M7 12.5l10 4.5"/>',
    inspect: '<rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M14 4.5v15"/><path d="M6.5 9h4M6.5 12h4"/>',
    'more-h': '<circle cx="5" cy="12" r="1.2" fill="currentColor"/><circle cx="12" cy="12" r="1.2" fill="currentColor"/><circle cx="19" cy="12" r="1.2" fill="currentColor"/>',
    drag: '<circle cx="9" cy="6" r="1.2" fill="currentColor"/><circle cx="15" cy="6" r="1.2" fill="currentColor"/><circle cx="9" cy="12" r="1.2" fill="currentColor"/><circle cx="15" cy="12" r="1.2" fill="currentColor"/><circle cx="9" cy="18" r="1.2" fill="currentColor"/><circle cx="15" cy="18" r="1.2" fill="currentColor"/>',
    undo: '<path d="M9 14 4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>',
    logout: '<path d="M10 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h5"/><path d="M15 8l4 4-4 4M19 12H9"/>',
    language: '<path d="M4 6h10M9 3.5V6M6 20l4-9 4 9M7.5 17h5"/><path d="M13 6a12 12 0 0 1-6 9M8 6a12 12 0 0 0 6 9"/><path d="M14 20h6"/>',
    kanban: '<rect x="3.5" y="4" width="5" height="16" rx="1"/><rect x="9.5" y="4" width="5" height="10" rx="1"/><rect x="15.5" y="4" width="5" height="13" rx="1"/>',
    settings2: '<path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2"/><circle cx="10" cy="17" r="2"/>',
  };

  function injectSprite() {
    if (document.getElementById('kodex-sprite')) return;
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.id = 'kodex-sprite';
    svg.setAttribute('aria-hidden', 'true');
    svg.style.cssText = 'position:absolute;width:0;height:0;overflow:hidden';
    svg.innerHTML = Object.keys(ICONS).map((k) => `<symbol id="i-${k}" viewBox="0 0 24 24">${ICONS[k]}</symbol>`).join('');
    document.body.insertBefore(svg, document.body.firstChild);
  }

  const ic = (name, size, cls) => `<svg class="ic${size ? ' ic-' + size : ''}${cls ? ' ' + cls : ''}" aria-hidden="true"><use href="#i-${name}"/></svg>`;
  const esc = (s) => String(s == null ? '' : s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  const el = (html) => { const t = document.createElement('template'); t.innerHTML = html.trim(); return t.content.firstElementChild; };
  const $ = (sel, root) => (root || document).querySelector(sel);
  const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

  /* ------------------------------------------------------------------
     Общие атомы (используются и оболочкой, и страницами)
  ------------------------------------------------------------------ */
  const PILL = {
    run: ['run', 'spin', 'ic-spin'], ok: ['ok', 'check'], done: ['ok', 'check'], wait: ['wait', 'clock'], queued: ['wait', 'clock'],
    gate: ['gate', 'shield'], err: ['err', 'alert'], off: ['off', 'minus'], info: ['info', 'info'], cancel: ['off', 'x-circle'], draft: ['info', 'edit'],
  };
  function pill(kind, text, sm) {
    const [mod, icon, extra] = PILL[kind] || PILL.info;
    return `<span class="pill pill--${mod}${sm ? ' pill--sm' : ''}">${ic(icon, null, extra)}${esc(text)}</span>`;
  }
  const SOURCES = {
    cc: ['monitor', 'Control Center', 'Запущено вручную из Control Center'],
    kodex: ['bot', 'Kodex', 'Запущено через помощника Kodex от имени пользователя'],
    auto: ['automation', 'Автоматизация', 'Запущено по расписанию автоматизации'],
    delegation: ['branch', 'Делегирование', 'Запущено другим ИИ-сотрудником по типизированному инструменту'],
    integration: ['plug', 'Интеграция', 'Запущено входящим событием интеграции'],
  };
  function source(kind, withLabel) {
    const [icon, label, tip] = SOURCES[kind] || SOURCES.cc;
    return `<span class="source" data-tip="${esc(tip)}" data-tip-pos="bottom" tabindex="0">${ic(icon)}${withLabel === false ? '' : esc(label)}</span>`;
  }
  function avatar(initials, size, tone, img) {
    return `<span class="avatar${size ? ' avatar--' + size : ''}${tone ? ' avatar--' + tone : ''}" aria-hidden="true">${img ? `<img src="${img}" alt="">` : esc(initials)}</span>`;
  }

  /* ------------------------------------------------------------------
     Синтетические данные
  ------------------------------------------------------------------ */
  function rng(seed) { let s = seed >>> 0 || 1; return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 4294967296; }; }
  const pick = (r, arr) => arr[Math.floor(r() * arr.length)];
  const DATA = {
    user: { name: 'Анна Волкова', initials: 'АВ', role: 'Владелец', email: 'a.volkova@company.example' },
    projects: [
      { ref: 'sales', name: 'Корпоративные продажи', sub: 'Предложения и аналитика клиентов' },
      { ref: 'support', name: 'Клиентская поддержка', sub: 'Обращения и база знаний' },
      { ref: 'legal', name: 'Юридический отдел', sub: 'Договоры и проверка рисков' },
      { ref: 'content', name: 'Маркетинг и контент', sub: 'Материалы и публикации' },
      { ref: 'finance', name: 'Бухгалтерия', sub: 'Сверки и платёжные документы' },
      { ref: 'hr', name: 'Кадровая служба', sub: 'Документы и адаптация сотрудников' },
      { ref: 'dev', name: 'Разработка платформы', sub: 'IT-Процессы и релизы' },
    ],
    agents: [
      { ref: 'a1', name: 'Аналитик продаж', initials: 'АП', purpose: 'Исследование клиентов и подготовка фактов', role: 'Аналитик', tone: 'ok', model: 'gpt-5.1', runtime: 'Стандартный', env: 'Офисные документы и PDF', state: 'ok' },
      { ref: 'a2', name: 'Редактор предложений', initials: 'РП', purpose: 'Итоговые коммерческие документы', role: 'Редактор', tone: 'acc', model: 'gpt-5.1', runtime: 'Стандартный', env: 'Офисные документы и PDF', state: 'run' },
      { ref: 'a3', name: 'Юридический консультант', initials: 'ЮК', purpose: 'Условия договоров и правовые риски', role: 'Консультант', tone: '', model: 'gpt-5.1-mini', runtime: 'Стандартный', env: 'Юридические документы (OCR)', state: 'gate' },
      { ref: 'a4', name: 'Менеджер продаж', initials: 'МП', purpose: 'Координация Процесса обработки лида', role: 'Координатор', tone: 'acc', model: 'gpt-5.1', runtime: 'Стандартный', env: 'Стандартное окружение', state: 'ok' },
      { ref: 'a5', name: 'Классификатор обращений', initials: 'КО', purpose: 'Сортировка входящих обращений по темам', role: 'Классификатор', tone: '', model: 'gpt-5.1-mini', runtime: 'Экономичный', env: 'Стандартное окружение', state: 'off' },
      { ref: 'a6', name: 'Исследователь рынка', initials: 'ИР', purpose: 'Сбор публичных сведений о компаниях', role: 'Исследователь', tone: 'ok', model: 'gpt-5.1', runtime: 'Стандартный', env: 'Веб-исследование (браузер)', state: 'ok' },
      { ref: 'a7', name: 'Автор писем', initials: 'АП', purpose: 'Черновики писем клиентам по шаблонам', role: 'Автор', tone: 'warn', model: 'gpt-5.1-mini', runtime: 'Экономичный', env: 'Стандартное окружение', state: 'err' },
      { ref: 'a8', name: 'Контролёр качества', initials: 'КК', purpose: 'Проверка документов перед отправкой', role: 'Контролёр', tone: '', model: 'gpt-5.1', runtime: 'Стандартный', env: 'Офисные документы и PDF', state: 'ok' },
    ],
    people: ['Анна Волкова', 'Михаил Орлов', 'Елена Крылова', 'Игорь Лебедев', 'Дмитрий Соловьёв', 'Ольга Панова', 'Сергей Титов', 'Наталья Громова', 'Павел Зайцев', 'Мария Фёдорова', 'Артём Беляев', 'Ксения Романова'],
    companies: ['Север', 'Восток', 'Юг', 'Запад', 'Меридиан', 'Прибой', 'Ладога', 'Онега', 'Терра', 'Вектор', 'Орбита', 'Спектр', 'Кварта', 'Импульс', 'Гранит'],
  };
  const FILE_STEMS = ['Описание продукта', 'Сценарии продаж', 'Коммерческое предложение', 'Прайс-лист 2026', 'Договор поставки', 'Протокол встречи', 'Анкета клиента', 'Сводка по лидам', 'Регламент обработки', 'Отчёт по обращениям', 'Презентация решения', 'Бриф', 'Техническое задание', 'Список контактов', 'Условия пилота'];
  const EXTS = [['pdf', 'pdf'], ['docx', 'doc'], ['xlsx', 'xls'], ['md', 'md'], ['png', 'img'], ['txt', 'txt'], ['csv', 'xls'], ['zip', 'zip']];
  function genFiles(n, seed) {
    const r = rng(seed || 7); const out = [];
    for (let i = 0; i < n; i++) {
      const stem = pick(r, FILE_STEMS); const [ext, mime] = pick(r, EXTS);
      const dup = r() < 0.18; const company = pick(r, DATA.companies);
      const size = Math.round(40 + r() * 5200); const ver = 1 + Math.floor(r() * 4);
      const scan = r() < 0.9 ? 'ok' : r() < 0.6 ? 'err' : 'run';
      const days = Math.floor(r() * 40); const src = r() < 0.7 ? 'Загрузил(а) ' + pick(r, DATA.people) : 'Результат запуска «' + pick(r, ['Подготовить предложение', 'Сводка по лидам', 'Проверка договора']) + '»';
      out.push({
        id: 'f' + i, name: `${stem}${dup ? '' : ' — ' + company}.${ext}`, ext, mime, sizeKb: size, ver,
        scan, when: days === 0 ? 'сегодня, ' + (8 + Math.floor(r() * 10)) + ':' + String(Math.floor(r() * 6)) + '0' : days === 1 ? 'вчера' : days + ' дн. назад',
        source: src, context: dup ? (r() < 0.5 ? 'Проект «Корпоративные продажи» · папка Клиенты/' + company : 'Результат запуска · ' + company) : 'Загружен вручную',
        bound: r() < 0.5 ? pick(r, DATA.agents).name : '',
      });
    }
    return out;
  }
  function genSessions(n, seed) {
    const r = rng(seed || 11); const out = [];
    const titles = ['Подготовить предложение для компании', 'Собрать факты о компании', 'Проверить договор с поставщиком', 'Разобрать обращения за неделю по клиенту', 'Классифицировать входящие письма от', 'Сравнить условия конкурентов для', 'Подготовить ответ на претензию компании'];
    for (let i = 0; i < n; i++) {
      const days = Math.floor(r() * 30); const ag = pick(r, DATA.agents);
      const st = pick(r, ['ok', 'ok', 'ok', 'err', 'cancel', 'gate']);
      out.push({ id: 's' + i, title: `${pick(r, titles)} ${pick(r, DATA.companies)}${r() < 0.3 ? ' с учётом обновлённого регламента и требований к пилотному внедрению' : ''}`, agent: ag.name, initials: ag.initials, tone: ag.tone, state: st, when: days === 0 ? 'сегодня' : days === 1 ? 'вчера' : days + ' дн. назад', turns: 1 + Math.floor(r() * 6), context: pick(r, ['Итог: черновик документа сохранён', 'Итог: подготовлены выводы и вопросы', 'Итог: файл результата доступен', 'Остановлено пользователем', 'Ошибка на последнем ходе']) });
    }
    return out;
  }
  const ENV_NAMES = ['Стандартное окружение', 'Офисные документы и PDF', 'Юридические документы (OCR)', 'Аналитика данных (Python)', 'Веб-исследование (браузер)', 'Таблицы и финансовые отчёты', 'Медиа и изображения', 'Переводы и локализация', 'Интеграции 1С (клиент)', 'Разработка: Go и Node'];
  function genEnvs(n, seed) {
    const r = rng(seed || 5); const out = [];
    for (let i = 0; i < n; i++) {
      const base = ENV_NAMES[i % ENV_NAMES.length]; const rev = 1 + Math.floor(r() * 9);
      const build = i < 10 ? (i === 4 ? 'building' : i === 7 ? 'failed' : 'ready') : pick(r, ['ready', 'ready', 'ready', 'building', 'failed']);
      out.push({ id: 'e' + i, name: i < ENV_NAMES.length ? base : `${base} · вариант ${Math.floor(i / ENV_NAMES.length) + 1}`, purpose: pick(r, ['Работа с офисными файлами', 'Разбор сканов и PDF', 'Расчёты и таблицы', 'Публичные источники', 'Общие задачи без специальных инструментов']), tools: pick(r, [['LibreOffice', 'pdftotext', 'pandoc'], ['tesseract', 'ocrmypdf', 'pdftk'], ['python 3.12', 'pandas', 'openpyxl'], ['chromium', 'curl'], ['bash', 'jq', 'git']]), rev, build, compatible: r() < 0.85, agents: Math.floor(r() * 6) });
    }
    return out;
  }
  function genUsers(n, seed) {
    const r = rng(seed || 3); const out = [];
    const first = ['Анна', 'Михаил', 'Елена', 'Игорь', 'Дмитрий', 'Ольга', 'Сергей', 'Наталья', 'Павел', 'Мария', 'Артём', 'Ксения', 'Виктор', 'Тимур', 'Полина'];
    const last = ['Волкова', 'Орлов', 'Крылова', 'Лебедев', 'Соловьёв', 'Панова', 'Титов', 'Громова', 'Зайцев', 'Фёдорова', 'Беляев', 'Романова', 'Кузнецов', 'Смирнова', 'Николаев'];
    for (let i = 0; i < n; i++) {
      const f = pick(r, first); const l = pick(r, last); const dep = pick(r, ['Продажи', 'Поддержка', 'Юридический отдел', 'Маркетинг', 'Финансы', 'Кадры', 'Разработка']);
      out.push({ id: 'u' + i, name: `${f} ${l}`, initials: f[0] + l[0], email: `${translit(f)[0]}.${translit(l)}@company.example`.toLowerCase(), dep, groups: pick(r, [['sales-all'], ['support-l1'], ['legal'], ['marketing', 'content'], ['finance'], []]) });
    }
    return out;
  }
  function translit(s) { const m = { а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z', и: 'i', й: 'y', к: 'k', л: 'l', м: 'm', н: 'n', о: 'o', п: 'p', р: 'r', с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'c', ч: 'ch', ш: 'sh', щ: 'sch', ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya' }; return s.toLowerCase().split('').map((c) => m[c] ?? c).join(''); }
  function genAgentsMany(n, seed) {
    const r = rng(seed || 9); const out = [];
    const roles = ['Аналитик', 'Редактор', 'Консультант', 'Координатор', 'Классификатор', 'Исследователь', 'Автор', 'Контролёр', 'Переводчик', 'Ассистент'];
    const areas = ['продаж', 'обращений', 'договоров', 'закупок', 'рынка', 'писем', 'качества', 'документов', 'отчётности', 'релизов'];
    for (let i = 0; i < n; i++) {
      const base = i < DATA.agents.length ? DATA.agents[i] : null;
      const name = base ? base.name : `${pick(r, roles)} ${pick(r, areas)}${r() < 0.3 ? ' · ' + pick(r, DATA.companies) : ''}`;
      const init = base ? base.initials : name.split(/[\s·]+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join('');
      out.push({ id: 'ag' + i, name, initials: init, tone: base ? base.tone : pick(r, ['', 'acc', 'ok', 'warn']), purpose: base ? base.purpose : pick(r, ['Подготовка документов', 'Разбор входящих данных', 'Проверка и контроль', 'Сбор сведений']), project: base ? 'Корпоративные продажи' : pick(r, DATA.projects).name, state: base ? base.state : pick(r, ['ok', 'ok', 'off']) });
    }
    return out;
  }

  /* ------------------------------------------------------------------
     Оболочка
  ------------------------------------------------------------------ */
  const GLOBAL_NAV = [
    ['home', 'Главная', 'home', '/'],
    ['projects', 'Проекты', 'folder', '/projects'],
    ['runs', 'Запуски', 'run', '/runs'],
    ['decisions', 'Решения', 'decision', '/decisions'],
    ['integrations', 'Интеграции', 'plug', '/integrations'],
  ];
  const PROJECT_NAV = [
    ['overview', 'Обзор', 'home'],
    ['agents', 'ИИ-сотрудники', 'bot'],
    ['workflows', 'Процессы', 'wf'],
    ['runs', 'Запуски', 'run'],
    ['files', 'Файлы', 'files'],
    ['automations', 'Автоматизации', 'automation'],
    ['environments', 'Окружения', 'layers'],
    ['members', 'Участники', 'users'],
  ];
  const CONN = { online: 'В сети', reconnecting: 'Переподключение…', offline: 'Нет соединения' };

  function connPill(state) {
    const s = state || 'online';
    return `<span class="conn" data-state="${s}" role="status" aria-live="polite" data-tip="${s === 'online' ? 'Обновления приходят в реальном времени' : s === 'reconnecting' ? 'Соединение восстанавливается, данные на экране актуальны на момент разрыва' : 'Показан последний подтверждённый снимок'}" data-tip-pos="bottom"><span class="conn__dot"></span><span class="conn__text">${CONN[s]}</span></span>`;
  }

  function navItem(key, label, icon, active, badge, sub) {
    return `<a class="nav-item${sub ? ' nav-item--sub' : ''}" href="#" data-nav="${key}" ${active ? 'aria-current="page"' : ''}><span class="nav-item__label">${ic(icon)}<span>${esc(label)}</span></span>${badge ? `<span class="badge-count">${badge}</span>` : ''}</a>`;
  }

  function desktopShell(cfg) {
    const project = cfg.project;
    const active = cfg.nav || 'home';
    const isProject = active.startsWith('project.');
    const projectKey = isProject ? active.slice(8) : null;
    const projectsMenu = `
      <div class="menu menu--wide" id="menu-project-switch">
        <div class="menu__search"><label class="search search--bg"><svg class="ic ic-14"><use href="#i-search"/></svg><input type="search" placeholder="Найти Проект" aria-label="Найти Проект" data-menu-filter></label></div>
        <div class="menu__title">Проекты</div>
        ${DATA.projects.map((p) => `<button class="menu__item" role="menuitemradio" aria-checked="${project && p.ref === project.ref}" data-filter-text="${esc(p.name.toLowerCase())}"><span class="menu__text"><span>${esc(p.name)}</span><small>${esc(p.sub)}</small></span>${project && p.ref === project.ref ? ic('check', 14) : ''}</button>`).join('')}
        <div class="menu__sep"></div>
        <a class="menu__item" href="#" role="menuitem">${ic('folder')}<span>Все Проекты</span></a>
        <a class="menu__item" href="#" role="menuitem">${ic('plus')}<span>Создать Проект</span></a>
      </div>`;
    const sidebar = `
      <aside class="sidebar" aria-label="Навигация">
        <div class="sidebar__brand"><span class="brand-mark"><svg viewBox="0 0 24 24"><path d="M4 18V7l8 5 8-5v11"/></svg></span><span class="sidebar__brand-name">Kodex</span></div>
        <nav class="sidebar__scroll">
          ${GLOBAL_NAV.map(([k, l, i]) => navItem(k, l, i, !isProject && active === k, k === 'decisions' ? cfg.decisions : 0)).join('')}
          <div class="sidebar__project">
            <div class="sidebar__group-title">Проект</div>
            <button class="project-switch" type="button" data-menu="#menu-project-switch" aria-haspopup="menu" aria-expanded="false" data-tip="Сменить Проект" data-tip-pos="bottom">
              <span class="project-switch__dot" ${project ? '' : 'style="background:var(--line-strong)"'}></span>
              <span class="project-switch__text"><span class="project-switch__name">${project ? esc(project.name) : 'Проект не выбран'}</span><span class="project-switch__hint">${project ? esc(project.sub || 'Активен') : 'Выберите Проект'}</span></span>
              ${ic('chev', 14)}
            </button>
            ${projectsMenu}
          </div>
          ${project ? `<div class="sidebar__nav-sub">${PROJECT_NAV.map(([k, l, i]) => navItem('project.' + k, l, i, isProject && projectKey === k, 0, true)).join('')}</div>` : `<div class="xs mut" style="padding:6px 12px 0;line-height:1.5">Разделы Проекта появятся после выбора Проекта.</div>`}
        </nav>
        <div class="sidebar__footer">${navItem('admin', 'Администрирование', 'gear', active === 'admin')}</div>
      </aside>`;
    const topbar = `
      <header class="topbar">
        <label class="search topbar__search"><svg class="ic ic-14"><use href="#i-search"/></svg><input type="search" id="global-search" placeholder="Поиск по Проектам, сотрудникам, Процессам и запускам" aria-label="Глобальный поиск" data-menu="#menu-global-search" data-menu-on="input"><span class="search__kbd" aria-hidden="true"><kbd>Ctrl</kbd><kbd>K</kbd></span></label>
        <div class="menu menu--wide" id="menu-global-search" style="width:560px;max-width:560px">
          <div class="menu__title">Результаты · серверный поиск · <span class="mono">3</span></div>
          <a class="menu__item" href="#">${ic('run')}<span class="menu__text"><span>Подготовить предложение для компании Север</span><small>Запуск · Корпоративные продажи · выполняется</small></span></a>
          <a class="menu__item" href="#">${ic('bot')}<span class="menu__text"><span>Аналитик продаж</span><small>ИИ-сотрудник · Корпоративные продажи</small></span></a>
          <a class="menu__item" href="#">${ic('wf')}<span class="menu__text"><span>Обработка нового лида</span><small>Процесс · v3 · Корпоративные продажи</small></span></a>
          <div class="menu__sep"></div>
          <div class="xs mut" style="padding:4px 10px 6px">Показываются только доступные вам объекты. Минимум 2 символа.</div>
        </div>
        <div class="topbar__actions">
          <a class="topbar__decisions" href="#" data-tip="Ожидают вашего решения" data-tip-pos="bottom">${ic('decision')}<span>Решения</span>${cfg.decisions ? `<span class="badge-count">${cfg.decisions}</span>` : ''}</a>
          ${connPill(cfg.connection)}
          <button class="user-menu-btn" type="button" data-menu="#menu-user" aria-haspopup="menu" aria-expanded="false" aria-label="Меню пользователя: ${esc(DATA.user.name)}">${avatar(DATA.user.initials, 28)}<span class="user-menu-btn__text"><span class="user-menu-btn__name">${esc(DATA.user.name)}</span><span class="user-menu-btn__role">${esc(DATA.user.role)}</span></span>${ic('chev', 14)}</button>
          <div class="menu" id="menu-user" data-align="right">
            <div class="menu__title">${esc(DATA.user.email)}</div>
            <button class="menu__item" role="menuitem">${ic('user')}<span>Профиль и уведомления</span></button>
            <button class="menu__item" role="menuitem">${ic('language')}<span>Язык</span><span class="menu__hint">Русский</span></button>
            <button class="menu__item" role="menuitem" data-theme-toggle>${ic('moon')}<span>Тема</span><span class="menu__hint" data-theme-label>Светлая</span></button>
            <div class="menu__sep"></div>
            <button class="menu__item" role="menuitem">${ic('logout')}<span>Выйти</span></button>
          </div>
        </div>
      </header>`;
    return { sidebar, topbar };
  }

  function mobileShell(cfg) {
    const active = cfg.nav || 'home';
    const tabs = [['home', 'Главная', 'home'], ['projects', 'Проекты', 'folder'], ['runs', 'Запуски', 'run'], ['decisions', 'Решения', 'decision']];
    const lead = cfg.back
      ? `<button class="m-icon" type="button" aria-label="Назад">${ic('back')}</button>`
      : `<button class="m-icon" type="button" aria-label="Меню" data-drawer="#m-nav-drawer">${ic('menu')}</button>`;
    const top = `
      <header class="m-top">
        ${lead}
        <div class="m-top__title"><h1>${esc(cfg.title || 'Kodex')}</h1>${cfg.sub ? `<div class="m-top__sub">${esc(cfg.sub)}</div>` : ''}</div>
        <button class="m-icon" type="button" aria-label="Поиск" data-drawer="#m-search-sheet">${ic('search')}</button>
        ${cfg.moreMenu ? `<button class="m-icon" type="button" aria-label="Ещё действия" data-menu="${cfg.moreMenu}" aria-haspopup="menu" aria-expanded="false">${ic('dots')}</button>` : ''}
      </header>`;
    const bottom = cfg.tabs === false ? '' : `<nav class="m-tabs" aria-label="Основные разделы">${tabs.map(([k, l, i]) => `<a href="#" ${active === k || (k === 'projects' && active.startsWith('project.')) ? 'aria-current="page"' : ''}>${ic(i)}<span>${l}</span>${k === 'decisions' && cfg.decisions ? `<span class="badge-count">${cfg.decisions}</span>` : ''}</a>`).join('')}</nav>`;
    const navDrawer = `
      <div class="drawer drawer--left" id="m-nav-drawer" role="dialog" aria-label="Навигация" data-modal-drawer style="width:300px">
        <div class="drawer__head"><div class="drawer__head-text row gap-10"><span class="brand-mark"><svg viewBox="0 0 24 24"><path d="M4 18V7l8 5 8-5v11"/></svg></span><span class="b" style="font-size:15px">Kodex</span></div><button class="close-btn" type="button" aria-label="Закрыть" data-close>${ic('x')}</button></div>
        <div class="drawer__body drawer__body--flush">
          <div style="padding:10px 12px" class="col gap-2">
            ${GLOBAL_NAV.map(([k, l, i]) => navItem(k, l, i, active === k, k === 'decisions' ? cfg.decisions : 0)).join('')}
            <div class="sidebar__group-title">Проект</div>
            ${cfg.project ? `<div class="project-switch" style="margin-bottom:6px"><span class="project-switch__dot"></span><span class="project-switch__text"><span class="project-switch__name">${esc(cfg.project.name)}</span><span class="project-switch__hint">Сменить Проект</span></span>${ic('chev', 14)}</div>${PROJECT_NAV.map(([k, l, i]) => navItem('project.' + k, l, i, active === 'project.' + k, 0, true)).join('')}` : '<div class="xs mut" style="padding:4px 12px">Проект не выбран</div>'}
            <div class="divider" style="margin:8px 0"></div>
            ${navItem('admin', 'Администрирование', 'gear', active === 'admin')}
          </div>
        </div>
        <div class="drawer__foot"><span class="row gap-10 grow">${avatar(DATA.user.initials, 32)}<span class="col gap-2"><span class="sm">${esc(DATA.user.name)}</span><span class="xs mut">${esc(DATA.user.role)}</span></span></span>${connPill(cfg.connection)}</div>
      </div>`;
    const searchSheet = `
      <div class="sheet sheet--full" id="m-search-sheet" role="dialog" aria-label="Поиск" data-modal-drawer>
        <div class="sheet__head"><label class="search search--bg grow"><svg class="ic ic-14"><use href="#i-search"/></svg><input type="search" placeholder="Поиск по всему доступному" aria-label="Поиск"></label><button class="m-icon" type="button" aria-label="Закрыть" data-close>${ic('x')}</button></div>
        <div class="sheet__body"><div class="xs mut">Введите минимум 2 символа. Поиск выполняется на сервере и показывает только доступные вам объекты.</div></div>
      </div>`;
    return { top, bottom, navDrawer, searchSheet };
  }

  function mount(cfg) {
    injectSprite();
    cfg = cfg || {};
    const frame = cfg.frame ? (typeof cfg.frame === 'string' ? $(cfg.frame) : cfg.frame) : $('.frame');
    if (!frame) return;
    const page = $('.page, .m-page', frame);
    const isMobile = frame.classList.contains('frame--mobile');
    let layer = $('.layer', frame);
    if (!layer) { layer = el('<div class="layer"></div>'); }
    if (isMobile) {
      const s = mobileShell(cfg);
      const shell = el('<div class="m-shell"></div>');
      shell.insertAdjacentHTML('beforeend', s.top);
      if (page) { page.classList.add('m-body'); shell.appendChild(page); }
      shell.insertAdjacentHTML('beforeend', s.bottom);
      frame.prepend(shell);
      layer.insertAdjacentHTML('beforeend', s.navDrawer + s.searchSheet);
    } else {
      const s = desktopShell(cfg);
      const shell = el('<div class="shell"></div>');
      shell.insertAdjacentHTML('beforeend', s.sidebar);
      const main = el('<div class="main"></div>');
      main.insertAdjacentHTML('beforeend', s.topbar);
      if (page) main.appendChild(page);
      shell.appendChild(main);
      frame.prepend(shell);
    }
    // FAB
    if (cfg.fab !== false) {
      const fab = el(`<button class="fab" type="button" aria-label="Открыть помощника Kodex" data-tip="Kodex · ${cfg.kodexState === 'busy' ? 'выполняет ваш запрос' : 'готов помочь на этом экране'}" data-tip-pos="left" data-state="${cfg.kodexState || 'ready'}">${ic('bot')}<span class="fab__state" aria-hidden="true"></span></button>`);
      frame.appendChild(fab);
      fab.addEventListener('click', () => openKodex(frame, cfg));
    }
    frame.appendChild(layer);
    if (!$('.toast-host', frame)) frame.appendChild(el('<div class="toast-host" aria-live="polite"></div>'));
    bindBehaviours(frame);
    if (cfg.theme) frame.setAttribute('data-theme', cfg.theme);
    // Режимы просмотра через hash: #tablet, #dark, #embed, #light, #reduce-motion (для validation.html)
    const hash = (location.hash || '').toLowerCase();
    if (hash.includes('tablet') && frame.classList.contains('frame--desktop')) { frame.classList.replace('frame--desktop', 'frame--tablet'); }
    if (hash.includes('dark')) frame.setAttribute('data-theme', 'dark');
    if (hash.includes('light')) frame.setAttribute('data-theme', 'light');
    if (hash.includes('embed')) document.body.classList.add('embed');
    if (hash.includes('reduce-motion')) frame.classList.add('reduce-motion');
    if (hash.includes('kodex')) cfg.kodexOpen = true;
    if (cfg.kodexOpen) openKodex(frame, cfg, true);
    frame.__kodex = cfg;
    return frame;
  }

  /* Kodex drawer / bottom sheet: страница может определить <template id="kodex-panel">;
     иначе показывается общий контекстный вариант. */
  function openKodex(frame, cfg, immediate) {
    const isMobile = frame.classList.contains('frame--mobile');
    let panel = $('#kodex-panel-live', frame);
    if (!panel) {
      const tpl = $('#kodex-panel', frame) || $('#kodex-panel');
      const ctx = (cfg && cfg.kodexContext) || { screen: 'Текущий экран', object: '', can: ['Ответить на вопрос об этом экране', 'Подготовить план изменений для проверки'] };
      const head = `
        <div class="${isMobile ? 'sheet__head' : 'drawer__head'}">
          ${isMobile ? '' : avatar('', 32, 'bot').replace('</span>', ic('bot') + '</span>')}
          <div class="${isMobile ? 'sheet__title' : 'drawer__head-text'}">
            <div class="drawer__title">Kodex</div>
            <div class="drawer__sub">Контекст: <b class="ink2">${esc(ctx.screen)}</b>${ctx.object ? ' · ' + esc(ctx.object) : ''}</div>
          </div>
          <button class="btn btn--icon btn--ghost" type="button" aria-label="История диалогов" data-tip="История диалогов" data-tip-pos="bottom" data-menu="#kodex-history-menu" aria-haspopup="menu" aria-expanded="false">${ic('history')}</button>
          <button class="close-btn" type="button" aria-label="Закрыть Kodex" data-close>${ic('x')}</button>
        </div>`;
      const body = tpl ? tpl.innerHTML : `
        <div class="${isMobile ? 'sheet__body' : 'drawer__body'} col gap-12">
          <div class="notice notice--info notice--sm">${ic('info')}<span class="notice__text">На этом экране Kodex может: ${ctx.can.map(esc).join('; ')}. Изменения будут записаны от вашего имени после проверки полномочий на сервере.</span></div>
          <div class="msg msg--bot"><div class="msg__meta">Kodex · сейчас</div><div class="msg__bubble">Чем помочь на этом экране? Могу объяснить состояние, подготовить план изменений или найти нужный объект.</div></div>
        </div>
        <div class="${isMobile ? 'sheet__foot' : 'drawer__foot'}" style="flex-direction:column;align-items:stretch">
          <div class="composer"><div class="composer__area"><textarea rows="2" placeholder="Спросите Kodex или опишите, что настроить" aria-label="Сообщение Kodex"></textarea><div class="composer__btns"><button class="composer__mic" type="button" disabled aria-label="Голосовой ввод появится позже" data-tip="Голосовой ввод появится позже">${ic('mic', 14)}</button><button class="composer__send" type="button" aria-label="Отправить">${ic('send', 14)}</button></div></div><div class="composer__bar"><span>Enter — отправить · Shift+Enter — новая строка</span></div></div>
        </div>`;
      const historyMenu = `<div class="menu menu--wide" id="kodex-history-menu" data-align="right"><div class="menu__title">История диалогов</div><button class="menu__item">${ic('plus')}<span>Новый диалог</span></button><div class="menu__sep"></div><button class="menu__item" aria-checked="true"><span class="menu__text"><span>Настройка отдела продаж</span><small>сегодня, 10:08 · Проект «Корпоративные продажи»</small></span></button><button class="menu__item"><span class="menu__text"><span>Почему запуск остановился?</span><small>вчера, 17:40 · Запуск «Проверка договора»</small></span></button><button class="menu__item"><span class="menu__text"><span>Подключение GitHub</span><small>20 августа · Интеграции</small></span></button></div>`;
      panel = el(isMobile
        ? `<div class="sheet sheet--full" id="kodex-panel-live" role="dialog" aria-label="Kodex" data-modal-drawer><div class="sheet__grip"></div>${head}${body}${historyMenu}</div>`
        : `<div class="drawer" id="kodex-panel-live" role="dialog" aria-label="Kodex" data-nonmodal>${head}${body}${historyMenu}</div>`);
      $('.layer', frame).appendChild(panel);
    }
    openOverlay(panel, immediate);
  }

  /* ------------------------------------------------------------------
     Overlay-механика: menu / drawer / sheet / modal, focus trap, Escape
  ------------------------------------------------------------------ */
  const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"]),[contenteditable="true"]';
  const stack = [];
  function frameOf(node) { return node.closest('.frame') || document.body; }

  function openOverlay(node, immediate, opener) {
    if (!node || node.classList.contains('is-open')) return;
    const frame = frameOf(node);
    const layer = $('.layer', frame) || frame;
    if (node.parentElement !== layer) layer.appendChild(node);
    const modal = !node.hasAttribute('data-nonmodal');
    let scrim = null;
    if (modal) {
      scrim = el('<div class="scrim"></div>');
      layer.insertBefore(scrim, node);
      scrim.addEventListener('click', () => closeOverlay(node));
      requestAnimationFrame(() => scrim.classList.add('is-open'));
    }
    node.hidden = false;
    if (immediate) node.style.transition = 'none';
    requestAnimationFrame(() => { node.classList.add('is-open'); if (immediate) setTimeout(() => (node.style.transition = ''), 50); });
    const rec = { node, scrim, opener: opener || document.activeElement, modal };
    stack.push(rec);
    const first = $('[autofocus]', node) || $$(FOCUSABLE, node).find((n) => !n.closest('.menu') && n.offsetParent !== null) || node;
    if (first === node) node.setAttribute('tabindex', '-1');
    setTimeout(() => first.focus({ preventScroll: true }), 30);
    node.dispatchEvent(new CustomEvent('kodex:open', { bubbles: true }));
  }
  function closeOverlay(node) {
    const idx = stack.findIndex((r) => r.node === node);
    if (idx < 0) return;
    const rec = stack.splice(idx, 1)[0];
    node.classList.remove('is-open');
    if (rec.scrim) { rec.scrim.classList.remove('is-open'); setTimeout(() => rec.scrim.remove(), 180); }
    if (rec.opener && rec.opener.focus) rec.opener.focus({ preventScroll: true });
    node.dispatchEvent(new CustomEvent('kodex:close', { bubbles: true }));
  }
  function closeTop() { if (stack.length) closeOverlay(stack[stack.length - 1].node); }

  function positionMenu(menu, trigger) {
    const frame = frameOf(trigger);
    const fr = frame.getBoundingClientRect(); const tr = trigger.getBoundingClientRect();
    menu.style.left = ''; menu.style.top = ''; menu.style.right = '';
    // menu живёт внутри .layer (absolute к frame)
    const layer = $('.layer', frame) || frame;
    if (menu.parentElement !== layer) layer.appendChild(menu);
    menu.classList.add('is-open');
    const mw = menu.offsetWidth, mh = menu.offsetHeight;
    let left = tr.left - fr.left; let top = tr.bottom - fr.top + 6;
    if (menu.dataset.align === 'right' || left + mw > fr.width - 8) left = Math.max(8, tr.right - fr.left - mw);
    if (top + mh > fr.height - 8) top = Math.max(8, tr.top - fr.top - mh - 6);
    if (menu.dataset.align === 'top') top = tr.top - fr.top - mh - 6;
    menu.style.left = left + 'px'; menu.style.top = top + 'px';
    if (trigger.closest('.search') && !menu.style.width) menu.style.minWidth = tr.width + 'px';
  }
  let openMenu = null;
  function showMenu(menu, trigger) {
    hideMenu();
    positionMenu(menu, trigger);
    openMenu = { menu, trigger };
    trigger.setAttribute('aria-expanded', 'true');
    const filter = $('[data-menu-filter]', menu);
    if (filter) { filter.value = ''; applyMenuFilter(menu, ''); filter.focus(); }
    else if (!trigger.matches('input')) { const first = $('.menu__item:not([disabled])', menu); if (first) first.focus(); }
  }
  function hideMenu() {
    if (!openMenu) return;
    const { menu, trigger } = openMenu;
    menu.classList.remove('is-open');
    trigger.setAttribute('aria-expanded', 'false');
    const restore = trigger;
    openMenu = null;
    if (document.activeElement && menu.contains(document.activeElement)) restore.focus({ preventScroll: true });
  }
  function applyMenuFilter(menu, q) {
    $$('[data-filter-text]', menu).forEach((item) => { item.hidden = q && !item.dataset.filterText.includes(q.toLowerCase()); });
  }

  function trapTab(e) {
    if (e.key !== 'Tab' || !stack.length) return;
    const rec = stack[stack.length - 1]; if (!rec.modal) return;
    const items = $$(FOCUSABLE, rec.node).filter((n) => n.offsetParent !== null && !n.closest('.menu:not(.is-open)'));
    if (!items.length) { e.preventDefault(); return; }
    const first = items[0], last = items[items.length - 1];
    if (e.shiftKey && (document.activeElement === first || !rec.node.contains(document.activeElement))) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }

  function bindBehaviours(root) {
    if (root.__kodexBound) return; root.__kodexBound = true;
    root.addEventListener('click', (e) => {
      const t = e.target.closest('[data-menu],[data-drawer],[data-sheet],[data-modal],[data-close],[data-toggle-class],[data-theme-toggle],[data-tabs] [role="tab"],[data-seg] .seg__btn,[data-chip-group] .chip,[data-select-row],[data-select-target],[data-nav],[data-view]');
      if (!t) return;
      if (t.matches('[data-nav]') || (t.matches('a') && t.getAttribute('href') === '#')) e.preventDefault();
      if (t.hasAttribute('data-menu') && t.dataset.menuOn !== 'input') {
        e.preventDefault(); e.stopPropagation();
        const menu = $(t.dataset.menu, root) || $(t.dataset.menu);
        if (openMenu && openMenu.menu === menu) hideMenu(); else if (menu) showMenu(menu, t);
        return;
      }
      if (t.hasAttribute('data-drawer') || t.hasAttribute('data-sheet') || t.hasAttribute('data-modal')) {
        e.preventDefault();
        const sel = t.dataset.drawer || t.dataset.sheet || t.dataset.modal;
        const node = $(sel, root) || $(sel);
        if (node) { hideMenu(); openOverlay(node, false, t); }
        return;
      }
      if (t.hasAttribute('data-close')) {
        e.preventDefault();
        const ov = t.closest('.drawer,.sheet,.modal-wrap,.modal,[data-overlay]');
        const menu = t.closest('.menu');
        if (menu) { hideMenu(); return; }
        if (ov) closeOverlay(ov.classList.contains('modal') && ov.parentElement.classList.contains('modal-wrap') ? ov.parentElement : ov);
        return;
      }
      if (t.hasAttribute('data-theme-toggle')) {
        const fr = frameOf(t); const dark = fr.getAttribute('data-theme') === 'dark';
        fr.setAttribute('data-theme', dark ? 'light' : 'dark');
        $$('[data-theme-label]', fr).forEach((n) => (n.textContent = dark ? 'Светлая' : 'Тёмная'));
        return;
      }
      if (t.hasAttribute('data-toggle-class')) {
        const target = t.dataset.target ? ($(t.dataset.target, root) || $(t.dataset.target)) : t;
        if (target) target.classList.toggle(t.dataset.toggleClass);
        if (t.hasAttribute('aria-pressed')) t.setAttribute('aria-pressed', t.getAttribute('aria-pressed') !== 'true');
        return;
      }
      if (t.matches('[data-tabs] [role="tab"]')) { e.preventDefault(); selectTab(t); return; }
      if (t.matches('[data-seg] .seg__btn')) {
        const seg = t.closest('[data-seg]');
        $$('.seg__btn', seg).forEach((b) => b.setAttribute('aria-pressed', b === t));
        if (seg.dataset.viewTarget) {
          const target = $(seg.dataset.viewTarget, root) || $(seg.dataset.viewTarget);
          if (target) { target.dataset.view = t.dataset.view; target.classList.toggle('is-grid', t.dataset.view === 'grid'); target.classList.toggle('is-list', t.dataset.view === 'list'); }
        }
        seg.dispatchEvent(new CustomEvent('kodex:seg', { bubbles: true, detail: { value: t.dataset.view || t.textContent.trim() } }));
        return;
      }
      if (t.matches('[data-chip-group] .chip')) {
        const g = t.closest('[data-chip-group]');
        if (g.dataset.chipGroup === 'multi') t.setAttribute('aria-pressed', t.getAttribute('aria-pressed') !== 'true');
        else $$('.chip', g).forEach((c) => c.setAttribute('aria-pressed', c === t));
        return;
      }
      if (t.hasAttribute('data-select-row')) {
        const group = t.closest('[data-select-group]') || t.parentElement;
        $$('[data-select-row]', group).forEach((r) => r.classList.toggle('is-selected', r === t));
        return;
      }
    });
    root.addEventListener('input', (e) => {
      const t = e.target;
      if (t.matches('[data-menu][data-menu-on="input"]')) {
        const menu = $(t.dataset.menu, root) || $(t.dataset.menu);
        if (t.value.trim().length >= 2) { if (!openMenu || openMenu.menu !== menu) showMenu(menu, t); }
        else hideMenu();
      }
      if (t.matches('[data-menu-filter]')) applyMenuFilter(t.closest('.menu'), t.value.trim());
      const search = t.closest('.search'); if (search) search.classList.toggle('has-value', !!t.value);
    });
    root.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        if (openMenu) { hideMenu(); e.stopPropagation(); return; }
        if (stack.length) { closeTop(); e.stopPropagation(); return; }
      }
      trapTab(e);
      if (openMenu && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
        const items = $$('.menu__item:not([disabled]):not([hidden])', openMenu.menu);
        if (!items.length) return;
        e.preventDefault();
        const i = items.indexOf(document.activeElement);
        const next = e.key === 'ArrowDown' ? items[(i + 1) % items.length] : items[(i - 1 + items.length) % items.length];
        next.focus();
      }
      if (e.target.matches('[role="tab"]') && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
        const tabs = $$('[role="tab"]', e.target.closest('[role="tablist"]'));
        const i = tabs.indexOf(e.target);
        const next = tabs[(i + (e.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length];
        selectTab(next); next.focus(); e.preventDefault();
      }
      if (e.key === 'Enter' && e.target.matches('.menu__item[role="menuitemradio"]')) { e.target.click(); }
    });
    document.addEventListener('mousedown', (e) => {
      if (openMenu && !openMenu.menu.contains(e.target) && !openMenu.trigger.contains(e.target)) hideMenu();
    });
    // Клик по пункту меню: закрыть (кроме data-keep)
    root.addEventListener('click', (e) => {
      const item = e.target.closest('.menu__item');
      if (item && openMenu && openMenu.menu.contains(item) && !item.hasAttribute('data-keep')) {
        if (item.getAttribute('role') === 'menuitemradio') $$('[role="menuitemradio"]', openMenu.menu).forEach((i) => i.setAttribute('aria-checked', i === item));
        hideMenu();
      }
    });
    $$('.search input', root).forEach((i) => { if (i.value) i.closest('.search').classList.add('has-value'); });
    $$('.search__clear', root).forEach((b) => b.addEventListener('click', () => { const i = $('input', b.closest('.search')); i.value = ''; i.dispatchEvent(new Event('input', { bubbles: true })); i.focus(); }));
    // Tabs init
    $$('[data-tabs]', root).forEach((tl) => { const cur = $('[role="tab"][aria-selected="true"]', tl) || $('[role="tab"]', tl); if (cur) selectTab(cur, true); });
    // Autosize textarea
    $$('textarea[data-autosize]', root).forEach((ta) => { const fit = () => { ta.style.height = 'auto'; ta.style.height = Math.min(ta.scrollHeight, 220) + 'px'; }; ta.addEventListener('input', fit); fit(); });
    // Ctrl+K
    root.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { const s = $('#global-search', root); if (s) { e.preventDefault(); s.focus(); } } });
  }

  function selectTab(tab, silent) {
    const list = tab.closest('[role="tablist"]');
    $$('[role="tab"]', list).forEach((t) => { const on = t === tab; t.setAttribute('aria-selected', on); t.tabIndex = on ? 0 : -1; const p = t.getAttribute('aria-controls') && document.getElementById(t.getAttribute('aria-controls')); if (p) p.hidden = !on; });
    if (!silent) list.dispatchEvent(new CustomEvent('kodex:tab', { bubbles: true, detail: { id: tab.id, controls: tab.getAttribute('aria-controls') } }));
  }

  /* ------------------------------------------------------------------
     Toast, realtime-симуляция
  ------------------------------------------------------------------ */
  function toast(frame, text, icon) {
    const host = $('.toast-host', frame) || frame;
    const t = el(`<div class="toast" role="status">${ic(icon || 'check-circle')}<span>${esc(text)}</span><button class="toast__close" type="button" aria-label="Скрыть">${ic('x', 14)}</button></div>`);
    host.appendChild(t);
    $('.toast__close', t).addEventListener('click', () => t.remove());
    setTimeout(() => t.remove(), 5000);
    return t;
  }
  function setConnection(frame, state) {
    $$('.conn', frame).forEach((c) => { c.dataset.state = state; $('.conn__text', c).textContent = CONN[state]; });
    $$('.refresh', frame).forEach((r) => { r.classList.toggle('is-stale', state !== 'online'); });
  }
  function simulateReconnect(frame, done) {
    setConnection(frame, 'reconnecting');
    $$('.refresh', frame).forEach((r) => r.classList.add('is-refreshing'));
    setTimeout(() => {
      setConnection(frame, 'online');
      $$('.refresh', frame).forEach((r) => { r.classList.remove('is-refreshing'); const t = $('.refresh__text', r); if (t) t.textContent = 'обновлено только что'; });
      toast(frame, 'Соединение восстановлено, пропущенные события догружены', 'wifi');
      if (done) done();
    }, 1800);
  }

  /* ------------------------------------------------------------------
     Infinite list (cursor pagination) и Async picker
  ------------------------------------------------------------------ */
  function infiniteList(opts) {
    const { scroll, list, items, render, pageSize = 40, footer, total, onChange } = opts;
    let cursor = 0, loading = false, done = false, query = '', filtered = items;
    const foot = footer || el('<div class="list-more"></div>');
    if (!footer) list.after(foot);
    function setFoot(html) { foot.innerHTML = html; }
    function load() {
      if (loading || done) return;
      loading = true;
      setFoot(`<span class="spinner spinner--sm"></span><span>Загружаем ещё…</span>`);
      setTimeout(() => {
        const chunk = filtered.slice(cursor, cursor + pageSize);
        chunk.forEach((it, i) => list.insertAdjacentHTML('beforeend', render(it, cursor + i)));
        cursor += chunk.length; loading = false;
        done = cursor >= filtered.length;
        setFoot(done ? `<span>Показано ${filtered.length} из ${filtered.length}</span>` : `<span>Показано ${cursor} из ${total || filtered.length}</span><span class="mut">· прокрутите для загрузки следующей страницы</span>`);
        if (onChange) onChange({ shown: cursor, total: filtered.length });
      }, 350);
    }
    function reset(q) {
      query = (q || '').trim().toLowerCase();
      filtered = query ? items.filter((it) => (opts.match ? opts.match(it, query) : JSON.stringify(it).toLowerCase().includes(query))) : items;
      cursor = 0; done = false; loading = false; list.innerHTML = '';
      if (!filtered.length) { setFoot(''); list.innerHTML = opts.empty || `<div class="empty empty--compact"><div class="empty__title">Ничего не найдено</div><div class="empty__text">Измените запрос или снимите фильтры.</div></div>`; return; }
      load();
    }
    scroll.addEventListener('scroll', () => { if (scroll.scrollTop + scroll.clientHeight >= scroll.scrollHeight - 80) load(); });
    reset('');
    return { reset, load, state: () => ({ cursor, total: filtered.length }) };
  }

  function debounce(fn, ms) { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; }

  /* asyncPicker: input/кнопка -> dropdown с серверным (симулированным) поиском */
  function asyncPicker(opts) {
    const { trigger, items, render, multi = false, placeholder = 'Поиск', pageSize = 30, onSelect, inline = false, host, renderSelected, errorQuery = 'ошибка', selected: initial = [] } = opts;
    const selected = new Map(initial.map((it) => [it.id, it]));
    const picker = el(`<div class="picker${inline ? ' picker--inline is-open' : ''}" role="dialog" aria-label="Выбор">
      <div class="picker__search"><label class="search search--bg"><svg class="ic ic-14"><use href="#i-search"/></svg><input type="search" placeholder="${esc(placeholder)}" aria-label="${esc(placeholder)}"><button class="search__clear" type="button" aria-label="Очистить">${ic('x', 12)}</button></label></div>
      <div class="picker__list" role="listbox" aria-multiselectable="${multi}"></div>
      <div class="picker__foot"></div>
      ${multi ? '<div class="picker__tray"><span class="picker__tray-count">Выбрано: 0</span></div>' : ''}
    </div>`);
    const input = $('input', picker), list = $('.picker__list', picker), foot = $('.picker__foot', picker), tray = $('.picker__tray', picker);
    let filtered = items, cursor = 0, loading = false, done = false, seq = 0;
    function item(it) {
      const on = selected.has(it.id);
      return `<button class="picker__item" role="option" type="button" aria-selected="${on}" data-id="${esc(it.id)}">${multi ? `<span class="picker__check">${ic('check')}</span>` : ''}${render(it)}</button>`;
    }
    function updateTray() {
      if (!tray) return;
      tray.innerHTML = `<span class="picker__tray-count">Выбрано: ${selected.size}</span>` + Array.from(selected.values()).slice(0, 6).map((it) => `<span class="chip is-active">${esc(renderSelected ? renderSelected(it) : it.name || it.title)}<button class="chip__x" type="button" aria-label="Убрать" data-unselect="${esc(it.id)}">${ic('x', 12)}</button></span>`).join('') + (selected.size > 6 ? `<span class="xs mut">и ещё ${selected.size - 6}</span>` : '');
    }
    function setFoot(html) { foot.innerHTML = html; }
    function loadMore() {
      if (loading || done) return; loading = true; const my = seq;
      setFoot('<span class="spinner spinner--sm"></span><span>Загружаем…</span>');
      setTimeout(() => {
        if (my !== seq) return;
        const chunk = filtered.slice(cursor, cursor + pageSize);
        chunk.forEach((it) => list.insertAdjacentHTML('beforeend', item(it)));
        cursor += chunk.length; done = cursor >= filtered.length; loading = false;
        setFoot(done ? `Показано ${filtered.length} из ${filtered.length}` : `Показано ${cursor} из ${filtered.length} · листайте дальше`);
      }, 320);
    }
    function search(q) {
      seq++; const my = seq; cursor = 0; done = false; loading = true;
      list.innerHTML = `<div class="picker__state"><span class="spinner"></span><span>Ищем на сервере…</span></div>`; setFoot('');
      setTimeout(() => {
        if (my !== seq) return; loading = false;
        if (q.toLowerCase() === errorQuery) {
          list.innerHTML = `<div class="picker__state">${ic('alert', 20)}<span>Не удалось получить список. Попробуйте ещё раз.</span><button class="btn btn--sm" type="button" data-retry>Повторить</button></div>`;
          $('[data-retry]', list).addEventListener('click', () => search(q === errorQuery ? '' : q));
          return;
        }
        const ql = q.toLowerCase();
        filtered = ql ? items.filter((it) => (opts.match ? opts.match(it, ql) : JSON.stringify(it).toLowerCase().includes(ql))) : items;
        list.innerHTML = '';
        if (!filtered.length) { list.innerHTML = `<div class="picker__state">${ic('search', 20)}<span>По запросу «${esc(q)}» ничего не найдено</span></div>`; return; }
        loadMore();
      }, 420);
    }
    const debounced = debounce((q) => search(q), 250);
    input.addEventListener('input', () => debounced(input.value.trim()));
    list.addEventListener('scroll', () => { if (list.scrollTop + list.clientHeight >= list.scrollHeight - 60) loadMore(); });
    list.addEventListener('click', (e) => {
      const b = e.target.closest('.picker__item'); if (!b) return;
      const it = items.find((x) => x.id === b.dataset.id); if (!it) return;
      if (multi) { if (selected.has(it.id)) selected.delete(it.id); else selected.set(it.id, it); b.setAttribute('aria-selected', selected.has(it.id)); updateTray(); if (onSelect) onSelect(Array.from(selected.values()), it); }
      else { selected.clear(); selected.set(it.id, it); $$('.picker__item', list).forEach((x) => x.setAttribute('aria-selected', x === b)); if (onSelect) onSelect(it); if (!inline) close(); }
    });
    if (tray) tray.addEventListener('click', (e) => { const x = e.target.closest('[data-unselect]'); if (!x) return; selected.delete(x.dataset.unselect); $$('.picker__item', list).forEach((b) => { if (b.dataset.id === x.dataset.unselect) b.setAttribute('aria-selected', 'false'); }); updateTray(); if (onSelect) onSelect(Array.from(selected.values())); });
    picker.addEventListener('keydown', (e) => {
      const opts2 = $$('.picker__item', list); const i = opts2.indexOf(document.activeElement);
      if (e.key === 'ArrowDown') { e.preventDefault(); (opts2[i + 1] || opts2[0])?.focus(); }
      if (e.key === 'ArrowUp') { e.preventDefault(); if (i <= 0) input.focus(); else opts2[i - 1].focus(); }
      if (e.key === 'Escape' && !inline) { e.stopPropagation(); close(); }
    });
    function open() {
      const frame = frameOf(trigger); const layer = $('.layer', frame) || frame;
      layer.appendChild(picker);
      const fr = frame.getBoundingClientRect(), tr = trigger.getBoundingClientRect();
      picker.classList.add('is-open');
      const w = opts.width || Math.max(tr.width, 360); picker.style.width = w + 'px';
      let left = tr.left - fr.left, top = tr.bottom - fr.top + 4;
      if (left + w > fr.width - 8) left = fr.width - 8 - w;
      if (top + picker.offsetHeight > fr.height - 8) top = tr.top - fr.top - picker.offsetHeight - 4;
      picker.style.left = left + 'px'; picker.style.top = top + 'px';
      trigger.setAttribute('aria-expanded', 'true');
      input.value = ''; search('');
      setTimeout(() => input.focus(), 20);
      const onDoc = (e) => { if (!picker.contains(e.target) && !trigger.contains(e.target)) close(); };
      setTimeout(() => document.addEventListener('mousedown', onDoc), 0);
      picker.__onDoc = onDoc;
    }
    function close() {
      picker.classList.remove('is-open'); trigger.setAttribute('aria-expanded', 'false');
      if (picker.__onDoc) document.removeEventListener('mousedown', picker.__onDoc);
      trigger.focus({ preventScroll: true });
    }
    if (inline) { (host || trigger).appendChild(picker); search(''); updateTray(); }
    else { trigger.setAttribute('aria-haspopup', 'dialog'); trigger.setAttribute('aria-expanded', 'false'); trigger.addEventListener('click', () => (picker.classList.contains('is-open') ? close() : open())); }
    updateTray();
    return { open, close, picker, selected: () => Array.from(selected.values()), search };
  }

  /* ------------------------------------------------------------------
     Canvas графа: pan / zoom / fit / minimap / выбор узла
  ------------------------------------------------------------------ */
  const NODE_STATE = {
    running: ['running', 'spin', 'Выполняется', 'ic-spin'], done: ['done', 'check', 'Завершён'], gate: ['gate', 'shield', 'Ждёт решения'],
    error: ['error', 'alert', 'Ошибка'], cancelled: ['cancelled', 'x-circle', 'Отменён'], pending: ['pending', 'clock', 'Не начат'], queued: ['pending', 'clock', 'В очереди'], waiting: ['pending', 'clock', 'Ожидает данных'],
  };
  function nodeHtml(n) {
    const [mod, icon, label, extra] = NODE_STATE[n.state] || NODE_STATE.pending;
    return `<button class="node node--${mod}${n.root ? ' node--root' : ''}" type="button" data-node="${esc(n.id)}" style="left:${n.x}px;top:${n.y}px;${n.w ? 'width:' + n.w + 'px;' : ''}" aria-label="${esc(n.kind)}: ${esc(n.title)}, ${esc(n.stateText || label)}">
      <span class="node__kind">${n.initials ? avatar(n.initials, 20, n.tone) : ''}${esc(n.kind)}</span>
      <span class="node__title">${esc(n.title)}</span>
      <span class="node__state">${ic(icon, null, extra)}${esc(n.stateText || label)}</span>
      ${n.phrase ? `<span class="node__phrase">${esc(n.phrase)}</span>` : ''}
      ${n.progress != null ? `<span class="progress"><span class="progress__bar" style="width:${n.progress}%"></span></span>` : ''}
      ${n.meta ? `<span class="node__meta">${esc(n.meta)}</span>` : ''}
    </button>`;
  }
  function canvas(opts) {
    const { root, nodes, edges = [], groups = [], selected, onSelect, minimap = true, legend = true, fitPadding = 40, nodeW = 176, nodeH = 96 } = opts;
    root.classList.add('canvas');
    root.innerHTML = '';
    const stage = el('<div class="canvas__stage"></div>');
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg'); svg.setAttribute('class', 'canvas__edges');
    stage.appendChild(svg);
    groups.forEach((g) => stage.insertAdjacentHTML('beforeend', `<div class="canvas__group" style="left:${g.x}px;top:${g.y}px;width:${g.w}px;height:${g.h}px"><span class="canvas__group-label">${esc(g.label)}</span></div>`));
    nodes.forEach((n) => stage.insertAdjacentHTML('beforeend', nodeHtml(n)));
    root.appendChild(stage);
    const byId = new Map(nodes.map((n) => [n.id, n]));
    function nodeBox(n) { const w = n.w || nodeW; const elx = $(`[data-node="${n.id}"]`, stage); const h = elx ? elx.offsetHeight : nodeH; return { x: n.x, y: n.y, w, h }; }
    function drawEdges() {
      svg.innerHTML = '';
      const labels = $$('.canvas__edge-label', stage); labels.forEach((l) => l.remove());
      let maxX = 0, maxY = 0;
      nodes.forEach((n) => { const b = nodeBox(n); maxX = Math.max(maxX, b.x + b.w); maxY = Math.max(maxY, b.y + b.h); });
      svg.setAttribute('width', maxX + 200); svg.setAttribute('height', maxY + 200);
      svg.innerHTML = `<defs><marker id="arr" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M0 0 7 3.5 0 7Z" fill="var(--edge)"/></marker><marker id="arr-live" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M0 0 7 3.5 0 7Z" fill="var(--edge-live)"/></marker></defs>`;
      edges.forEach((e) => {
        const a = byId.get(e.from), b = byId.get(e.to); if (!a || !b) return;
        const ab = nodeBox(a), bb = nodeBox(b);
        const x1 = ab.x + ab.w, y1 = ab.y + ab.h / 2, x2 = bb.x, y2 = bb.y + bb.h / 2;
        const dx = Math.max(30, (x2 - x1) / 2);
        const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
        p.setAttribute('d', `M${x1} ${y1} C${x1 + dx} ${y1} ${x2 - dx} ${y2} ${x2} ${y2}`);
        p.setAttribute('class', `${e.live ? 'is-live' : ''} ${e.dashed ? 'is-dashed' : ''} ${e.muted ? 'is-muted' : ''}`);
        p.setAttribute('marker-end', e.live ? 'url(#arr-live)' : 'url(#arr)');
        svg.appendChild(p);
        if (e.label) stage.insertAdjacentHTML('beforeend', `<span class="canvas__edge-label${e.live ? ' is-live' : ''}" style="left:${(x1 + x2) / 2}px;top:${(y1 + y2) / 2 - 8}px">${esc(e.label)}</span>`);
      });
      return { maxX, maxY };
    }
    let tx = 0, ty = 0, scale = 1;
    const apply = () => { stage.style.transform = `translate(${tx}px, ${ty}px) scale(${scale})`; $$('.canvas__zoom-value', root).forEach((z) => (z.textContent = Math.round(scale * 100) + '%')); updateMinimap(); };
    function bounds() { let minX = Infinity, minY = Infinity, maxX = 0, maxY = 0; nodes.forEach((n) => { const b = nodeBox(n); minX = Math.min(minX, b.x); minY = Math.min(minY, b.y); maxX = Math.max(maxX, b.x + b.w); maxY = Math.max(maxY, b.y + b.h); }); groups.forEach((g) => { minX = Math.min(minX, g.x); minY = Math.min(minY, g.y); maxX = Math.max(maxX, g.x + g.w); maxY = Math.max(maxY, g.y + g.h); }); return { minX, minY, maxX, maxY }; }
    function fit() {
      const b = bounds(); const W = root.clientWidth, H = root.clientHeight;
      const bw = b.maxX - b.minX + fitPadding * 2, bh = b.maxY - b.minY + fitPadding * 2;
      scale = Math.min(1.25, Math.max(0.4, Math.min(W / bw, H / bh)));
      tx = (W - (b.maxX - b.minX) * scale) / 2 - b.minX * scale; ty = (H - (b.maxY - b.minY) * scale) / 2 - b.minY * scale;
      apply();
    }
    function zoomAt(f, cx, cy) { const ns = Math.min(2, Math.max(0.35, scale * f)); const k = ns / scale; tx = cx - (cx - tx) * k; ty = cy - (cy - ty) * k; scale = ns; apply(); }
    // pan
    let drag = null;
    root.addEventListener('pointerdown', (e) => { if (e.button !== 0 || e.target.closest('.node,.canvas__toolbar,.minimap,.canvas__zoom,.canvas__legend')) return; drag = { x: e.clientX, y: e.clientY, tx, ty }; root.classList.add('is-panning'); root.setPointerCapture(e.pointerId); });
    root.addEventListener('pointermove', (e) => { if (!drag) return; tx = drag.tx + (e.clientX - drag.x); ty = drag.ty + (e.clientY - drag.y); apply(); });
    const endDrag = () => { drag = null; root.classList.remove('is-panning'); };
    root.addEventListener('pointerup', endDrag); root.addEventListener('pointercancel', endDrag);
    root.addEventListener('wheel', (e) => { e.preventDefault(); const r = root.getBoundingClientRect(); zoomAt(e.deltaY < 0 ? 1.12 : 0.89, e.clientX - r.left, e.clientY - r.top); }, { passive: false });
    root.addEventListener('keydown', (e) => { const step = 40; if (e.key === 'ArrowLeft') { tx += step; apply(); } if (e.key === 'ArrowRight') { tx -= step; apply(); } if (e.key === 'ArrowUp') { ty += step; apply(); } if (e.key === 'ArrowDown') { ty -= step; apply(); } if (e.key === '+' || e.key === '=') zoomAt(1.15, root.clientWidth / 2, root.clientHeight / 2); if (e.key === '-') zoomAt(0.87, root.clientWidth / 2, root.clientHeight / 2); if (e.key === '0') fit(); });
    root.tabIndex = 0; root.setAttribute('aria-label', 'Граф выполнения: перетаскивание — перемещение, колесо — масштаб, клавиши-стрелки и +/−/0');
    // selection
    let sel = selected || null;
    function select(id, silent) { sel = id; $$('.node', stage).forEach((n) => n.classList.toggle('is-selected', n.dataset.node === id)); updateMinimap(); if (!silent && onSelect) onSelect(byId.get(id)); }
    stage.addEventListener('click', (e) => { const n = e.target.closest('.node'); if (n) select(n.dataset.node); });
    // toolbar
    const tb = el(`<div class="canvas__toolbar">
      <button class="btn btn--sm btn--icon" type="button" aria-label="Уменьшить" data-tip="Уменьшить" data-tip-pos="bottom" data-act="out">${ic('zoom-out')}</button>
      <button class="btn btn--sm btn--icon" type="button" aria-label="Увеличить" data-tip="Увеличить" data-tip-pos="bottom" data-act="in">${ic('zoom-in')}</button>
      <button class="btn btn--sm" type="button" data-act="fit" data-tip="Показать весь граф" data-tip-pos="bottom">${ic('fit')}По размеру</button>
    </div>`);
    root.appendChild(tb);
    tb.addEventListener('click', (e) => { const b = e.target.closest('[data-act]'); if (!b) return; if (b.dataset.act === 'fit') fit(); else zoomAt(b.dataset.act === 'in' ? 1.2 : 0.83, root.clientWidth / 2, root.clientHeight / 2); });
    root.appendChild(el(`<div class="canvas__zoom"><span class="canvas__zoom-value">100%</span></div>`));
    if (legend) root.appendChild(el(`<div class="canvas__legend"><span><span class="dot-state dot-state--run"></span>работает</span><span><span class="dot-state dot-state--ok"></span>завершён</span><span><span class="dot-state dot-state--warn"></span>решение</span><span><span class="dot-state dot-state--err"></span>ошибка</span><span><span class="dot-state"></span>не начат</span></div>`));
    // minimap
    let mm = null;
    if (minimap) { mm = el('<div class="minimap" aria-hidden="true"></div>'); root.appendChild(mm); mm.addEventListener('click', (e) => { const b = bounds(); const r = mm.getBoundingClientRect(); const k = Math.min(mm.clientWidth / (b.maxX - b.minX + 80), mm.clientHeight / (b.maxY - b.minY + 80)); const gx = (e.clientX - r.left) / k + b.minX - 40, gy = (e.clientY - r.top) / k + b.minY - 40; tx = root.clientWidth / 2 - gx * scale; ty = root.clientHeight / 2 - gy * scale; apply(); }); }
    function updateMinimap() {
      if (!mm) return; const b = bounds(); const pad = 40; const gw = b.maxX - b.minX + pad * 2, gh = b.maxY - b.minY + pad * 2;
      const k = Math.min(mm.clientWidth / gw, mm.clientHeight / gh); const ox = (mm.clientWidth - gw * k) / 2, oy = (mm.clientHeight - gh * k) / 2;
      mm.innerHTML = nodes.map((n) => { const bb = nodeBox(n); const st = n.state === 'running' ? ' is-running' : n.state === 'gate' ? ' is-gate' : n.state === 'error' ? ' is-error' : ''; return `<span class="minimap__node${st}" style="left:${ox + (bb.x - b.minX + pad) * k}px;top:${oy + (bb.y - b.minY + pad) * k}px;width:${bb.w * k}px;height:${bb.h * k}px"></span>`; }).join('');
      const vx = (-tx / scale - b.minX + pad) * k + ox, vy = (-ty / scale - b.minY + pad) * k + oy, vw = (root.clientWidth / scale) * k, vh = (root.clientHeight / scale) * k;
      mm.insertAdjacentHTML('beforeend', `<span class="minimap__view" style="left:${vx}px;top:${vy}px;width:${vw}px;height:${vh}px"></span>`);
    }
    drawEdges();
    requestAnimationFrame(() => { drawEdges(); fit(); if (sel) select(sel, true); });
    return { fit, select, redraw: () => { drawEdges(); apply(); }, getView: () => ({ tx, ty, scale }), setView: (v) => { tx = v.tx; ty = v.ty; scale = v.scale; apply(); }, zoomAt, stage, update: (id, patch) => { const n = byId.get(id); if (!n) return; Object.assign(n, patch); const old = $(`[data-node="${id}"]`, stage); const fresh = el(nodeHtml(n)); old.replaceWith(fresh); if (sel === id) fresh.classList.add('is-selected'); drawEdges(); updateMinimap(); } };
  }

  /* ------------------------------------------------------------------
     Простая подсветка Markdown / TOML для code-editor shell
  ------------------------------------------------------------------ */
  function hlMarkdown(src) {
    return src.split('\n').map((line) => {
      let s = esc(line);
      if (/^#{1,3} /.test(line)) s = `<span class="hl-h">${s}</span>`;
      s = s.replace(/\{\{\s*[\w.]+\s*\}\}/g, (m) => `<span class="hl-var">${m}</span>`);
      s = s.replace(/\*\*([^*]+)\*\*/g, '<span class="hl-b">**$1**</span>');
      s = s.replace(/(^|\s)(- |\d+\. )/g, '$1<span class="hl-kw">$2</span>');
      return `<span class="code__line">${s || ' '}</span>`;
    }).join('');
  }
  function hlValue(v) {
    // значение после "=" или ":" — строки, булевы, числа; экранирование до подсветки
    let s = esc(v);
    s = s.replace(/&quot;([^&]*?)&quot;/g, '<span class="hl-str">&quot;$1&quot;</span>');
    s = s.replace(/\b(true|false)\b/g, '<span class="hl-kw">$1</span>');
    s = s.replace(/(^|[\s\[,])(-?\d+(?:\.\d+)?)(?=$|[\s\],])/g, '$1<span class="hl-num">$2</span>');
    return s;
  }
  function hlToml(src) {
    return src.split('\n').map((line) => {
      if (/^\s*#/.test(line)) return `<span class="code__line hl-cm">${esc(line)}</span>`;
      if (/^\s*\[/.test(line)) return `<span class="code__line"><span class="hl-sec">${esc(line)}</span></span>`;
      const m = line.match(/^(\s*[\w.\-"]+)(\s*=\s*)(.*)$/);
      if (m) return `<span class="code__line"><span class="hl-key">${esc(m[1])}</span>${esc(m[2])}${hlValue(m[3])}</span>`;
      return `<span class="code__line">${esc(line) || ' '}</span>`;
    }).join('');
  }
  function hlYaml(src) {
    const KW = /\b(required|read|write|destructive|financial|external_communication|platform_admin|code_change|managed-http|managed-mcp|managed-command)\b/g;
    return src.split('\n').map((line) => {
      if (/^\s*#/.test(line)) return `<span class="code__line hl-cm">${esc(line)}</span>`;
      const m = line.match(/^(\s*-?\s*)([\w.\-\/]+)(:)(\s*)(.*)$/);
      if (m) return `<span class="code__line">${esc(m[1])}<span class="hl-key hl-kw">${esc(m[2])}</span>${esc(m[3] + m[4])}${hlValue(m[5]).replace(KW, '<span class="hl-num">$1</span>')}</span>`;
      const l = line.match(/^(\s*-\s*)(.*)$/);
      if (l) return `<span class="code__line">${esc(l[1])}${hlValue(l[2])}</span>`;
      return `<span class="code__line">${esc(line) || ' '}</span>`;
    }).join('');
  }
  function codeBlock(src, lang, opts) {
    opts = opts || {};
    const lines = src.split('\n');
    const gutter = lines.map((_, i) => `<span class="${opts.errLines && opts.errLines.includes(i + 1) ? 'is-err' : ''}">${i + 1}</span>`).join('\n');
    const body = lang === 'toml' ? hlToml(src) : lang === 'yaml' ? hlYaml(src) : hlMarkdown(src);
    return `<div class="code__body"><pre class="code__gutter" aria-hidden="true">${gutter}</pre><pre class="code__text" ${opts.editable ? 'contenteditable="true" spellcheck="false" role="textbox" aria-multiline="true"' : ''} aria-label="${esc(opts.label || 'Код')}">${body}</pre></div>`;
  }

  const fmtKb = (kb) => (kb >= 1024 ? (kb / 1024).toFixed(1).replace('.', ',') + ' МБ' : kb + ' КБ');

  global.Kodex = { mount, ic, esc, el, $, $$, pill, source, avatar, data: DATA, gen: { files: genFiles, sessions: genSessions, envs: genEnvs, users: genUsers, agents: genAgentsMany, rng, pick }, openOverlay, closeOverlay, closeTop, showMenu, hideMenu, toast, setConnection, simulateReconnect, infiniteList, asyncPicker, canvas, nodeHtml, codeBlock, hlMarkdown, hlToml, hlYaml, fmtKb, injectSprite, bindBehaviours, selectTab, openKodex, debounce };
  document.addEventListener('DOMContentLoaded', injectSprite);
})(window);
