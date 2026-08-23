import * as L from './_lib.mjs';
const T = L.T;

const tabs = (items, active) => `
  <div style="display: flex; gap: 4px; padding: 0 24px; border-bottom: 1px solid ${T.line}; background: ${T.bg}; flex: 0 0 42px; align-items: stretch;">
    ${items.map((t) => `<span style="display: flex; align-items: center; padding: 0 12px; font-size: 12.5px; ${t === active ? `color: ${T.ink}; font-weight: 600; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${t}</span>`).join('')}
  </div>`;

const fileRow = (name, meta, pill, ver, src, bind, selected) => `
  <div style="display: flex; align-items: center; gap: 13px; padding: 12px 16px; border-top: 1px solid ${T.row}; ${selected ? `background: ${T.accTint}; box-shadow: inset 2px 0 0 ${T.acc};` : ''}">
    <span style="color: ${T.sec}; display: flex;">${L.icon('file', 18)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 13px; font-weight: ${selected ? 600 : 500};">${name}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${meta} · ${src}</span>
    </span>
    <span style="flex: 0 0 200px; font-size: 11.5px; color: ${T.sec};">${bind}</span>
    <span style="flex: 0 0 60px; font-family: ${T.mono}; font-size: 11.5px; color: ${T.mut};">${ver}</span>
    <span style="flex: 0 0 170px; display: flex; justify-content: flex-end;">${pill}</span>
  </div>`;

const meta = (k, v) => `<div style="display: flex; gap: 12px; padding: 8px 0; border-top: 1px solid ${T.row};"><span style="flex: 0 0 96px; font-size: 12px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${v}</span></div>`;

// ============ UX-13 Файлы и знания ============
export const d13 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('files')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'Файлы и знания'],
    title: 'Файлы и знания',
    sub: 'Материалы для ИИ-сотрудников и результаты их работы',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Загрузить файл</button>`,
  })}
  ${tabs(['Файлы', 'Источники знаний', 'Результаты'], 'Файлы')}

  <div style="flex: 1 1 auto; display: flex; gap: 18px; padding: 16px 24px 22px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 12px; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 8px;">
        ${L.searchBox('Найти файл', '280px')}
        ${['Все типы', 'Все состояния', 'Все источники'].map((f) => `<button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">${f} ${L.icon('chev', 13)}</button>`).join('')}
      </div>

      <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden; display: flex; flex-direction: column; min-height: 0;">
        <div style="display: flex; align-items: center; gap: 13px; padding: 10px 16px; background: ${T.field}; font-size: 11px; letter-spacing: 0.04em; text-transform: uppercase; color: ${T.mut};">
          <span style="flex: 1 1 auto;">Файл</span><span style="flex: 0 0 200px;">Используется</span><span style="flex: 0 0 60px;">Версия</span><span style="flex: 0 0 170px; text-align: right;">Состояние</span>
        </div>
        ${fileRow('Описание продукта.pdf', 'PDF · 4,2 МБ', L.statusPill('done', 'Проверен', 'sm'), 'v2', 'Загрузила Анна Волкова', 'Аналитик продаж +1', true)}
        ${fileRow('Сценарии продаж.docx', 'DOCX · 820 КБ', L.statusPill('run', 'Проверяется', 'sm'), 'v1', 'Загрузил Михаил Орлов', '—', false)}
        ${fileRow('Коммерческое предложение.pdf', 'PDF · 1,4 МБ', L.statusPill('done', 'Результат запуска', 'sm'), 'v1', 'Запуск «Подготовить предложение»', 'Редактор предложений', false)}
        ${fileRow('Прайс-лист 2026.xlsx', 'XLSX · 260 КБ', L.statusPill('err', 'Изолирован', 'sm'), 'v1', 'Загрузил Михаил Орлов', '—', false)}
        <div style="flex: 1 1 auto;"></div>
        <div style="padding: 11px 16px; border-top: 1px solid ${T.row}; background: ${T.subtle}; font-size: 11.5px; color: ${T.mut};">
          Пока проверка не завершена, файл нельзя связать с ИИ-сотрудником, использовать в запуске или скачать.
        </div>
      </div>
    </div>

    <aside style="flex: 0 0 380px; display: flex; flex-direction: column; gap: 14px;">
      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
        <div style="height: 168px; background: ${T.canvas}; border-bottom: 1px solid ${T.line}; padding: 16px 18px; display: flex; flex-direction: column; gap: 7px; overflow: hidden;">
          <span style="height: 9px; width: 62%; border-radius: 4px; background: ${T.lineStrong};"></span>
          <span style="height: 7px; width: 92%; border-radius: 4px; background: ${T.line};"></span>
          <span style="height: 7px; width: 88%; border-radius: 4px; background: ${T.line};"></span>
          <span style="height: 7px; width: 74%; border-radius: 4px; background: ${T.line};"></span>
          <span style="height: 7px; width: 90%; border-radius: 4px; background: ${T.line}; margin-top: 8px;"></span>
          <span style="height: 7px; width: 66%; border-radius: 4px; background: ${T.line};"></span>
          <span style="margin-top: auto; font-size: 11px; color: ${T.mut};">Безопасный предпросмотр первой страницы</span>
        </div>
        <div style="padding: 14px 16px;">
          <div style="font-size: 14px; font-weight: 600;">Описание продукта.pdf</div>
          <div style="margin-top: 8px;">${L.statusPill('done', 'Проверен', 'sm')}</div>
          <div style="margin-top: 10px;">
            ${meta('Тип', 'PDF')}
            ${meta('Размер', '4,2 МБ')}
            ${meta('Версия', 'v2 · загружена сегодня')}
            ${meta('Источник', 'Загрузила Анна Волкова')}
            ${meta('Добавлен', '22 августа, 09:41')}
            ${meta('Используется', 'Аналитик продаж, Редактор предложений')}
          </div>
        </div>
      </div>

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 16px; display: flex; flex-direction: column; gap: 9px;">
        <button style="width: 100%; height: 36px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13px; font-weight: 600;">Связать с ИИ-сотрудником</button>
        <div style="display: flex; gap: 8px;">
          <button style="flex: 1 1 0; height: 32px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Использовать в запуске</button>
          <button style="flex: 0 0 96px; height: 32px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Скачать</button>
        </div>
        <button style="width: 100%; height: 32px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 12.5px;">Загрузить новую версию</button>
      </div>
    </aside>
  </div>`,
}));
