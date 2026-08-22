import * as L from './_lib.mjs';
const T = L.T;

const runCard = (task, project, target, pill, work, meta, action) => L.mCard(`
  <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 9px;">
    <span style="flex: 1 1 auto; min-width: 0; font-size: 14px; font-weight: 600; line-height: 1.3;">${task}</span>
    ${pill}
  </div>
  <div style="font-size: 11.5px; color: ${T.mut}; margin-top: 7px; line-height: 1.4;">${project} · ${target}</div>
  ${work ? `<div style="font-size: 12.5px; color: ${T.ink2}; margin-top: 6px; line-height: 1.4;">${work}</div>` : ''}
  <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 10px;">
    <span style="font-size: 11px; color: ${T.mut};">${meta}</span>
    ${action}
  </div>`);

const act = (t, pri) => `<button style="height: 44px; padding: 0 16px; border-radius: 10px; ${pri ? `border: 0; background: ${T.acc}; color: #FFFFFF; font-weight: 600;` : `border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font-weight: 500;`} font: inherit; font-size: 13px;">${t}</button>`;

// ============ UX-11 mobile ============
export const m11 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Запуски' })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 10px; background: ${T.subtle}; overflow: hidden;">
    ${L.mBtn('Новый запуск', 'pri')}
    <div style="display: flex; align-items: center; gap: 8px;">
      <button style="height: 38px; display: flex; align-items: center; gap: 7px; padding: 0 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">${L.icon('filter', 15)}Фильтры<span style="min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: ${T.acc}; color: #FFFFFF; font-size: 10.5px; font-weight: 600; display: flex; align-items: center; justify-content: center;">2</span></button>
      ${L.chip('Активные', true)}${L.chip('Все')}
    </div>

    ${runCard('Подготовить предложение для компании Север', 'Корпоративные продажи', 'Процесс · Обработка нового лида',
      L.statusPill('run', 'Выполняется', 'sm'), 'Редактор предложений: собираю итоговую структуру',
      '12 мин · Control Center', act('Открыть', false))}

    ${runCard('Разобрать 24 обращения клиентов', 'Клиентская поддержка', 'Классификатор обращений',
      L.statusPill('gate', 'Ожидает решения', 'sm'), '',
      '38 мин · Расписание', act('Рассмотреть', true))}

    ${runCard('Подготовить недельную сводку', 'Корпоративные продажи', 'Аналитик продаж',
      L.statusPill('done', 'Успешно', 'sm'), `<span style="display: inline-flex; align-items: center; gap: 6px;">${L.icon('file', 14)}Сводка.pdf</span>`,
      'вчера · Помощник MatterCodex', act('Открыть', false))}

    ${runCard('Проверить условия договора', 'Корпоративные продажи', 'Юридический консультант',
      L.statusPill('err', 'Ошибка', 'sm'), 'Исполнение остановлено',
      'вчера · Делегировано агентом', act('Повторить', false))}
  </div>
`));

// ============ UX-12 mobile ============
const node = (indent, name, kind, pill, phrase, meta, sel) => `
  <div style="display: flex; gap: 9px; padding-left: ${indent}px;">
    ${indent > 0 ? `<span style="flex: 0 0 1px; background: ${T.line}; margin: 4px 0;"></span>` : ''}
    <div style="flex: 1 1 auto; min-width: 0; border: ${sel ? `1.5px solid ${T.acc}` : `1px solid ${T.line}`}; border-radius: 11px; background: ${T.bg}; padding: 11px;">
      <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 9px;">
        <span style="flex: 1 1 auto; min-width: 0; font-size: 13.5px; font-weight: 600; line-height: 1.3;">${name}</span>
        ${pill}
      </div>
      <div style="font-size: 10.5px; letter-spacing: 0.05em; text-transform: uppercase; color: ${T.mut}; margin-top: 5px;">${kind} · ${meta}</div>
      ${phrase ? `<div style="font-size: 12.5px; color: ${T.ink2}; margin-top: 7px; line-height: 1.4;">${phrase}</div>` : ''}
    </div>
  </div>`;

export const m12 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Предложение · Север', back: true, sub: 'Выполняется · 12 мин · в сети', right: 'dots' })}
  ${L.mTabs(['Граф', 'События', 'Результат', 'Файлы'], 'Граф')}

  <div style="flex: 1 1 auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 9px; background: ${T.subtle}; overflow: hidden;">
    <div style="display: flex; gap: 8px;">
      ${L.chip('Свернуть завершённые')}${L.chip('Выбранная ветка')}
    </div>

    ${node(0, 'Обработка нового лида', 'Процесс', L.statusPill('run', 'Выполняется', 'sm'), '', '12:04 · попытка 1', false)}
    ${node(12, 'Аналитик продаж', 'ИИ-сотрудник', L.statusPill('done', 'Завершён', 'sm'), 'Исследование клиента готово', '6 мин', false)}

    <div style="padding-left: 12px;">
      <div style="border: 1px dashed ${T.lineStrong}; border-radius: 12px; padding: 9px; background: ${T.bg};">
        <div style="font-size: 10.5px; letter-spacing: 0.05em; text-transform: uppercase; color: ${T.mut}; padding: 0 2px 7px;">Параллельно · передано обоим</div>
        <div style="display: flex; flex-direction: column; gap: 8px;">
          ${node(0, 'Редактор предложений', 'ИИ-сотрудник', L.statusPill('run', 'Выполняется', 'sm'), 'Собираю итоговую структуру документа', 'ход 1 · 4 мин', true)}
          ${node(0, 'Юридический консультант', 'ИИ-сотрудник', L.statusPill('wait', 'Ожидает данных', 'sm'), '', 'не начат', false)}
        </div>
      </div>
    </div>

    ${node(12, 'Согласовать предложение', 'Решение человека', L.statusPill('gate', 'Предстоит', 'sm'), '', 'решает владелец', false)}
  </div>

  ${L.mBottom(`
    <div style="display: flex; align-items: center; gap: 10px;">
      ${L.avatar('РП', 32, 'acc')}
      <span style="flex: 1 1 auto; min-width: 0;">
        <span style="display: block; font-size: 13.5px; font-weight: 600;">Редактор предложений</span>
        <span style="display: block; font-size: 11.5px; color: ${T.mut};">Выбран · ход 1 · попытка 1</span>
      </span>
      <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 18)}</span>
    </div>
    ${L.mBtn('Открыть диалог узла', 'sec')}`)}
`));
