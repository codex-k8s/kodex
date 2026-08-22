import * as L from './_lib.mjs';
const T = L.T;

const m = (v, l) => `<div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 11px; background: ${T.bg}; padding: 8px 10px;"><div style="font-size: 16px; font-weight: 600; letter-spacing: -0.02em;">${v}</div>
  <div style="font-size: 11px; color: ${T.mut}; margin-top: 1px;">${l}</div></div>`;

const collapsed = (t, note) => `
  <div style="display: flex; align-items: center; gap: 10px; min-height: 52px; padding: 0 14px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg};">
    <span style="flex: 1 1 auto; font-size: 14px; font-weight: 500;">${t}</span>
    <span style="font-size: 12px; color: ${T.mut};">${note}</span>
    <span style="color: ${T.mut}; display: flex;">${L.icon('chev', 18)}</span>
  </div>`;

// ============ UX-05 mobile ============
export const m05 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Корпоративные продажи', back: true })}
  <div style="flex: 0 0 auto; padding: 10px 16px; border-bottom: 1px solid ${T.line}; background: ${T.bg};">
    <button style="width: 100%; height: 44px; display: flex; align-items: center; gap: 9px; padding: 0 12px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px;">
      Разделы проекта<span style="color: ${T.mut}; margin-left: 4px;">·</span><span style="font-weight: 600;">Обзор</span>
      <span style="margin-left: auto; color: ${T.mut}; display: flex;">${L.icon('chev', 16)}</span>
    </button>
  </div>

  <div style="flex: 1 1 auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 9px; background: ${T.subtle}; overflow: hidden;">
    ${L.mBtn("Запустить работу", "pri")}
    <div style="display: flex; gap: 9px;">${m('3', 'ИИ-сотрудника')}${m('2', 'Процесса')}${m('1', 'запуск')}${m('1', 'решение')}</div>

    ${L.mCard(`
      <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px;">
        <span style="font-size: 11.5px; font-weight: 600; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.sec};">Сейчас выполняется</span>
        ${L.statusPill('run', 'Выполняется', 'sm')}
      </div>
      <div style="font-size: 15px; font-weight: 600; margin-top: 10px; line-height: 1.3;">Подготовить предложение для компании Север</div>
      <div style="font-size: 12px; color: ${T.mut}; margin-top: 6px;"><span style="font-family: ${T.mono};">12 мин</span> · Редактор предложений</div>
      <div style="font-size: 12.5px; color: ${T.ink2}; margin-top: 8px; line-height: 1.4;">Собираю итоговую структуру документа</div>
      <div style="margin-top: 10px;"><button style="width: 100%; height: 44px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px; font-weight: 500;">Открыть запуск</button></div>`)}

    ${L.mCard(`
      <div style="display: flex; align-items: center; gap: 8px; font-size: 11.5px; font-weight: 600; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.warn};">${L.icon('shield', 14)}Требует решения</div>
      <div style="font-size: 14.5px; font-weight: 600; margin-top: 8px;">Согласовать коммерческое предложение</div>
      <div style="margin-top: 10px;"><button style="width: 100%; height: 44px; border-radius: 10px; border: 0; background: ${T.warn}; color: #FFFFFF; font: inherit; font-size: 14px; font-weight: 600;">Рассмотреть</button></div>`,
      `border-color: ${T.warnLine}; background: ${T.warnTint};`)}

    ${collapsed('Команда', '3')}
    ${collapsed('Процессы', '2')}

    <div style="border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 12px;">
      <div style="display: flex; align-items: center; gap: 9px; font-size: 13.5px; font-weight: 600; color: ${T.ok};">${L.icon('check', 17)}Можно запускать работу</div>
      <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 6px;">Интеграции не подключены — это не мешает работе.</div>
    </div>
  </div>
`));

// ============ UX-06 mobile ============
const agentCard = (init, tone, name, role, pill, note, caps, ver, run) => L.mCard(`
  <div style="display: flex; align-items: flex-start; gap: 12px;">
    ${L.avatar(init, 38, tone)}
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 15px; font-weight: 600;">${name}</span>
      <span style="display: block; font-size: 12.5px; color: ${T.sec}; margin-top: 3px; line-height: 1.4;">${role}</span>
    </span>
    <span style="color: ${T.mut}; display: flex; padding-top: 3px;">${L.icon('dots', 18)}</span>
  </div>
  <div style="margin-top: 11px;">${pill}</div>
  ${note ? `<div style="font-size: 12.5px; color: ${T.ink2}; margin-top: 8px; line-height: 1.4;">${note}</div>` : ""}
  <div style="font-size: 11.5px; color: ${T.sec}; margin-top: 8px; line-height: 1.4;">${caps.join(" · ")}</div>
  <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 12px; padding-top: 12px; border-top: 1px solid ${T.row};">
    <span style="font-size: 11.5px; color: ${T.mut};">${ver}</span>
    <span style="display: flex; gap: 8px;">
      ${run ? `<button style="height: 44px; padding: 0 16px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px;">Запустить</button>` : ''}
      <button style="height: 44px; padding: 0 18px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px; font-weight: 500;">Открыть</button>
    </span>
  </div>`);

export const m06 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'ИИ-сотрудники', back: true, sub: 'Корпоративные продажи' })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">    ${L.mBtn('Создать ИИ-сотрудника', 'pri')}
        ${agentCard('АП', 'neutral', 'Аналитик продаж', 'Исследует клиента и готовит факты', L.statusPill('done', 'Готов', 'sm'),
      'Нет активной задачи', ['Работа с файлами', 'Анализ данных', 'Без интеграций'], 'Инструкции v3', true)}
    ${agentCard('РП', 'acc', 'Редактор предложений', 'Готовит итоговые документы', L.statusPill('run', 'Выполняет задачу', 'sm'),
      'Собираю итоговую структуру документа', ['Работа с файлами', 'Без интеграций'], 'Инструкции v5', false)}
    ${agentCard('ЮК', 'neutral', 'Юридический консультант', 'Проверяет условия и риски', L.statusPill('off', 'Отключён владельцем', 'sm'),
      '', ['Анализ документов', 'Без интеграций'], 'Инструкции v2', false)}
  </div>
`));
