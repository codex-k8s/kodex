import * as L from './_lib.mjs';
const T = L.T;

const agentCard = (init, tone, name, role, pill, work, caps, integr, ver, actions) => `
  <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px; display: flex; flex-direction: column; gap: 12px;">
    <div style="display: flex; align-items: flex-start; gap: 12px;">
      ${L.avatar(init, 36, tone)}
      <div style="flex: 1 1 auto; min-width: 0;">
        <div style="font-size: 15px; font-weight: 600;">${name}</div>
        <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 3px; line-height: 1.4;">${role}</div>
      </div>
      <span style="flex: 0 0 auto; color: ${T.mut}; display: flex;">${L.icon('dots', 16)}</span>
    </div>
    <div>${pill}</div>
    <div style="font-size: 12.5px; color: ${T.ink2}; line-height: 1.45; min-height: 34px;">${work}</div>
    <div style="display: flex; flex-wrap: wrap; gap: 6px;">
      ${caps.map((c) => `<span style="display: inline-flex; align-items: center; height: 24px; padding: 0 9px; border-radius: 6px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11.5px; color: ${T.sec};">${c}</span>`).join('')}
      <span style="display: inline-flex; align-items: center; height: 24px; padding: 0 9px; border-radius: 6px; background: ${T.bg}; border: 1px dashed ${T.lineStrong}; font-size: 11.5px; color: ${T.mut};">${integr}</span>
    </div>
    <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; padding-top: 12px; border-top: 1px solid ${T.row};">
      <span style="font-size: 11.5px; color: ${T.mut};">${ver}</span>
      <span style="display: flex; gap: 8px;">${actions}</span>
    </div>
  </div>`;

// ============ UX-06 ИИ-сотрудники ============
export const d06 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('agents')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'ИИ-сотрудники'],
    title: 'ИИ-сотрудники',
    sub: 'Настройте роли, инструкции и доступные возможности',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Создать ИИ-сотрудника</button>`,
  })}

  <div style="flex: 1 1 auto; padding: 18px 24px 24px; display: flex; flex-direction: column; gap: 16px; background: ${T.subtle}; min-height: 0;">
    <div style="display: flex; align-items: center; gap: 10px;">
      ${L.searchBox('Найти по имени или назначению', '340px')}
      <button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Все состояния ${L.icon('chev', 13)}</button>
      <button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Все возможности ${L.icon('chev', 13)}</button>
      <div style="flex: 1 1 auto;"></div>
      <span style="display: flex; align-items: center; border: 1px solid ${T.line}; border-radius: 8px; overflow: hidden;">
        <span aria-label="Карточками" style="width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; background: ${T.accTint}; color: ${T.accDark};">${L.icon('grid', 15)}</span>
        <span style="width: 1px; height: 32px; background: ${T.line};"></span>
        <span aria-label="Списком" style="width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; background: ${T.bg}; color: ${T.sec};">${L.icon('list', 15)}</span>
      </span>
    </div>

    <div style="display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px;">
      ${agentCard('АП', 'neutral', 'Аналитик продаж', 'Исследует клиента и готовит факты для предложения',
        L.statusPill('done', 'Готов', 'sm'), 'Нет активной задачи',
        ['Работа с файлами', 'Анализ данных'], 'Без интеграций',
        'Инструкции v3 · изменены сегодня', `${L.btn('Запустить', 'sec', 30)}${L.btn('Открыть', 'sec', 30)}`)}
      ${agentCard('РП', 'acc', 'Редактор предложений', 'Готовит итоговые документы',
        L.statusPill('run', 'Выполняет задачу', 'sm'), 'Собираю итоговую структуру документа',
        ['Работа с файлами', 'Подготовка документов'], 'Без интеграций',
        'Инструкции v5 · изменены вчера', `${L.btn('Открыть', 'sec', 30)}`)}
      ${agentCard('ЮК', 'neutral', 'Юридический консультант', 'Проверяет условия и риски',
        L.statusPill('off', 'Отключён владельцем', 'sm'), 'Запуск недоступен, пока агент отключён',
        ['Анализ документов'], 'Без интеграций',
        'Инструкции v2 · изменены 19 августа', `${L.btn('Открыть', 'sec', 30)}`)}
    </div>

    <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 18px; display: flex; align-items: center; gap: 12px;">
      <span style="color: ${T.acc}; display: flex;">${L.icon('bot', 18)}</span>
      <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.sec}; line-height: 1.45;">
        Глобальный <span style="color: ${T.ink}; font-weight: 500;">Помощник Kodex</span> не входит в команду Проекта: он системный и доступен из любого раздела.
      </span>
      ${L.btn('Открыть помощника', 'sec', 32)}
    </div>
  </div>`,
}));
