import * as L from './_lib.mjs';
const T = L.T;

const th = (t, w) => `<span style="flex: ${w}; font-size: 11px; letter-spacing: 0.04em; text-transform: uppercase; color: ${T.mut};">${t}</span>`;
const td = (t, w, extra = '') => `<span style="flex: ${w}; min-width: 0; ${extra}">${t}</span>`;

const row = (task, project, target, pill, work, dur, upd, action) => `
  <div style="display: flex; align-items: center; gap: 14px; padding: 13px 18px; border-top: 1px solid ${T.row};">
    ${td(`<span style="display: block; font-size: 13.5px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${task}</span>`, '1 1 0')}
    ${td(`<span style="font-size: 12.5px; color: ${T.sec};">${project}</span>`, '0 0 180px')}
    ${td(`<span style="font-size: 12.5px; color: ${T.sec};">${target}</span>`, '0 0 190px')}
    ${td(pill, '0 0 168px')}
    ${td(work, '0 0 250px')}
    ${td(`<span style="font-family: ${T.mono}; font-size: 12px; color: ${T.sec};">${dur}</span>`, '0 0 66px')}
    ${td(`<span style="font-size: 12px; color: ${T.mut};">${upd}</span>`, '0 0 86px')}
    ${td(action, '0 0 104px', 'display: flex; justify-content: flex-end;')}
  </div>`;

// ============ UX-11 Запуски ============
export const d11 = L.page(L.shellDesktop({
  nav: 'runs', project: 'Все проекты',
  body: `
  ${L.contentHead({
    path: ['Запуски'],
    title: 'Запуски',
    sub: 'Следите за текущей работой и возвращайтесь к результатам',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Новый запуск</button>`,
  })}

  <div style="flex: 1 1 auto; padding: 16px 24px 22px; display: flex; flex-direction: column; gap: 14px; background: ${T.subtle}; min-height: 0;">
    <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
      ${L.searchBox('Задача, агент или Процесс', '280px')}
      ${['Проект: Все', 'Состояние: Активные и недавние', 'Цель: Все', 'Источник: Все источники', 'Дата: 7 дней'].map((f) => `<button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">${f} ${L.icon('chev', 13)}</button>`).join('')}
      <button style="height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 12.5px;">Очистить фильтры</button>
    </div>

    <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden; display: flex; flex-direction: column; min-height: 0;">
      <div style="display: flex; align-items: center; gap: 14px; padding: 11px 18px; background: ${T.field};">
        ${th('Задача', '1 1 0')}${th('Проект', '0 0 180px')}${th('Цель', '0 0 190px')}${th('Состояние', '0 0 168px')}${th('Текущая работа', '0 0 250px')}${th('Длит.', '0 0 66px')}${th('Обновлено', '0 0 86px')}${th('', '0 0 104px')}
      </div>

      ${row('Подготовить предложение для компании Север', 'Корпоративные продажи', 'Процесс · Обработка нового лида',
        L.statusPill('run', 'Выполняется', 'sm'),
        `<span style="display: block; font-size: 12px; color: ${T.ink2}; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">Редактор предложений: собираю итоговую структуру</span><span style="display: block; font-size: 11px; color: ${T.mut}; margin-top: 2px;">Источник: Control Center</span>`,
        '12 мин', 'только что', L.btn('Открыть', 'sec', 28))}

      ${row('Разобрать 24 обращения клиентов', 'Клиентская поддержка', 'ИИ-сотрудник · Классификатор обращений',
        L.statusPill('gate', 'Ожидает решения', 'sm'),
        `<span style="display: block; font-size: 12px; color: ${T.ink2};">Нужно ваше решение</span><span style="display: block; font-size: 11px; color: ${T.mut}; margin-top: 2px;">Источник: Расписание</span>`,
        '38 мин', '4 мин назад', L.btn('Рассмотреть', 'pri', 28))}

      ${row('Подготовить недельную сводку', 'Корпоративные продажи', 'ИИ-сотрудник · Аналитик продаж',
        L.statusPill('done', 'Успешно', 'sm'),
        `<span style="display: flex; align-items: center; gap: 7px; font-size: 12px; color: ${T.ink2};">${L.icon('file', 14)}Сводка.pdf</span><span style="display: block; font-size: 11px; color: ${T.mut}; margin-top: 2px;">Источник: Помощник MatterCodex</span>`,
        '9 мин', 'вчера', L.btn('Открыть', 'sec', 28))}

      ${row('Проверить условия договора', 'Корпоративные продажи', 'ИИ-сотрудник · Юридический консультант',
        L.statusPill('err', 'Ошибка', 'sm'),
        `<span style="display: block; font-size: 12px; color: ${T.ink2};">Исполнение остановлено</span><span style="display: block; font-size: 11px; color: ${T.mut}; margin-top: 2px;">Источник: Делегировано агентом</span>`,
        '2 мин', 'вчера', L.btn('Повторить', 'sec', 28))}

      ${row('Собрать отчёт по обращениям', 'Клиентская поддержка', 'Процесс · Подготовка ответа клиенту',
        L.statusPill('done', 'Успешно', 'sm'),
        `<span style="display: flex; align-items: center; gap: 7px; font-size: 12px; color: ${T.ink2};">${L.icon('file', 14)}Отчёт по обращениям.pdf</span><span style="display: block; font-size: 11px; color: ${T.warn}; margin-top: 2px;">Уведомление в Mattermost не доставлено</span>`,
        '14 мин', 'вчера', L.btn('Открыть', 'sec', 28))}

      <div style="flex: 1 1 auto;"></div>
      <div style="display: flex; align-items: center; justify-content: space-between; padding: 11px 18px; border-top: 1px solid ${T.row}; background: ${T.subtle};">
        <span style="font-size: 11.5px; color: ${T.mut};">Список обновляется автоматически: новые события меняют только затронутую строку.</span>
        <span style="font-size: 11.5px; color: ${T.mut};">Показано 5 из 27</span>
      </div>
    </div>
  </div>`,
}));
