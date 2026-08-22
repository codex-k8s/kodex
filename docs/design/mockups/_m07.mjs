import * as L from './_lib.mjs';
const T = L.T;

const collapsed = (t, note) => `
  <div style="display: flex; align-items: center; gap: 10px; min-height: 52px; padding: 0 14px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg};">
    <span style="flex: 1 1 auto; font-size: 14px; font-weight: 500;">${t}</span>
    <span style="font-size: 12px; color: ${T.mut};">${note}</span>
    <span style="color: ${T.mut}; display: flex;">${L.icon('chev', 18)}</span>
  </div>`;

// ============ UX-07 mobile ============
export const m07 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Аналитик продаж', back: true, sub: 'Корпоративные продажи', right: 'dots' })}

  <div style="flex: 0 0 auto; padding: 14px 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <div style="display: flex; align-items: center; gap: 12px;">
      ${L.avatar('АП', 44)}
      <span style="flex: 1 1 auto; min-width: 0;">
        <span style="display: block; font-size: 16px; font-weight: 600;">Аналитик продаж</span>
        <span style="display: block; font-size: 12.5px; color: ${T.sec}; margin-top: 2px;">Специалист по исследованию клиентов</span>
      </span>
    </div>
    <div style="display: flex; align-items: center; gap: 8px; margin-top: 11px;">
      ${L.statusPill('done', 'Готов', 'sm')}
      <span style="display: inline-flex; align-items: center; height: 22px; padding: 0 9px; border-radius: 11px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11.5px; color: ${T.sec};">Инструкции v3</span>
    </div>
    <div style="margin-top: 12px;">${L.mBtn('Запустить', 'pri')}</div>
  </div>

  ${L.mTabs(['Обзор', 'Инструкции', 'Возможности', 'Версии'], 'Инструкции')}

  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">
    <div style="display: flex; align-items: center; gap: 10px;">
      <span style="font-size: 15px; font-weight: 600;">Черновик v4</span>
      <span style="display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 10px; border-radius: 12px; background: ${T.warnTint}; border: 1px solid ${T.warnLine}; color: ${T.warn}; font-size: 11.5px; font-weight: 500;">${L.icon('alert', 12)}Не опубликовано</span>
    </div>

    <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden;">
      <div style="padding: 9px 13px; border-bottom: 1px solid ${T.hair}; background: ${T.field}; font-size: 11.5px; color: ${T.sec};">Инструкции ИИ-сотрудника</div>
      <div style="padding: 12px; font-size: 13px; line-height: 1.6; color: ${T.ink};">Изучи предоставленные сведения о клиенте. Отделяй подтверждённые факты от предположений. Подготовь краткое резюме, потребности, риски и вопросы для уточнения. Не отправляй данные во внешние системы без явного разрешения.</div>
    </div>

    <div style="border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 13px;">
      <div style="font-size: 11.5px; font-weight: 600; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.sec};">Проверка</div>
      <div style="display: flex; flex-direction: column; gap: 8px; margin-top: 10px;">
        <span style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon('check', 14)}</span>Структура корректна</span>
        <span style="display: flex; align-items: center; gap: 8px; font-size: 12.5px; color: ${T.ink2};"><span style="color: ${T.ok}; display: flex;">${L.icon("check", 14)}</span>Секретных значений нет</span>
      </div>
    </div>

    ${collapsed('Runtime и возможности', 'Стандартный')}
      </div>

  ${L.mBottom(`
    ${L.mBtn('Опубликовать v4', 'pri')}
    ${L.mBtn('Проверить инструкции', 'sec')}`)}
`));

// ============ UX-08 mobile ============
const wfCard = (name, purpose, pill, coord, parts, gates, changed, canRun) => L.mCard(`
  <div style="display: flex; align-items: flex-start; gap: 12px;">
    <span style="width: 32px; height: 32px; flex: 0 0 32px; border-radius: 9px; background: ${T.field}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon("wf", 17)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 15px; font-weight: 600; line-height: 1.3;">${name}</span>
      <span style="display: block; font-size: 12.5px; color: ${T.sec}; margin-top: 4px; line-height: 1.45;">${purpose}</span>
    </span>
  </div>
  <div style="margin-top: 11px;">${pill}</div>
  <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 9px; line-height: 1.4;">Координатор: <span style="color: ${T.sec};">${coord}</span> · участников: <span style="color: ${T.sec};">${parts}</span> · решений: <span style="color: ${T.sec};">${gates}</span> · ${changed}</div>
  <div style="display: flex; gap: 8px; margin-top: 10px; padding-top: 10px; border-top: 1px solid ${T.row};">
    <button style="flex: 1 1 0; height: 44px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px; font-weight: 500;">Открыть</button>
    ${canRun
      ? `<button style="flex: 1 1 0; height: 44px; border-radius: 10px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Запустить</button>`
      : `<button style="flex: 1 1 0; height: 44px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.field}; color: ${T.faint}; font: inherit; font-size: 13.5px;">Запустить</button>`}
  </div>`);

export const m08 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Процессы', back: true })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">    ${L.mBtn('Создать Процесс', 'pri')}
        ${wfCard('Обработка нового лида', 'Исследовать компанию, подготовить предложение и получить решение владельца',
      L.statusPill('done', 'Опубликован · v3', 'sm'), 'Менеджер продаж', '3', '1', 'сегодня', true)}
    ${wfCard('Подготовка ответа клиенту', 'Собрать контекст обращения и подготовить ответ',
      L.statusPill('done', 'Опубликован · v1', 'sm'), 'Менеджер продаж', '2', 'нет', '19 августа', true)}
    ${wfCard('Согласование договора', 'Проверить условия и провести два согласования',
      L.statusPill('wait', 'Черновик · 2 замечания', 'sm'), 'Менеджер продаж', '3', '2', 'вчера', false)}
  </div>
`));
