import * as L from './_lib.mjs';
const T = L.T;

const fileCard = (name, meta, pill, src, bind) => L.mCard(`
  <div style="display: flex; align-items: flex-start; gap: 12px;">
    <span style="color: ${T.sec}; display: flex; padding-top: 2px;">${L.icon('file', 20)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 14px; font-weight: 600; line-height: 1.3; word-break: break-word;">${name}</span>
      <span style="display: block; font-size: 12px; color: ${T.mut}; margin-top: 4px;">${meta}</span>
    </span>
    <span style="color: ${T.mut}; display: flex; padding-top: 2px;">${L.icon('chevR', 18)}</span>
  </div>
  <div style="margin-top: 10px;">${pill}</div>
  <div style="font-size: 12px; color: ${T.mut}; margin-top: 9px; line-height: 1.45;">${src}${bind ? ` · используется: <span style="color: ${T.sec};">${bind}</span>` : ''}</div>`);

// ============ UX-13 mobile ============
export const m13 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Файлы и знания', back: true, sub: 'Корпоративные продажи' })}
  ${L.mTabs(['Файлы', 'Знания', 'Результаты'], 'Файлы')}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">    ${L.mBtn('Загрузить файл', 'pri')}
    <div style="display: flex; align-items: center; gap: 8px;">
      <button style="height: 40px; display: flex; align-items: center; gap: 8px; padding: 0 13px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13px;">${L.icon('filter', 15)}Фильтры</button>
      ${L.chip('Все типы', true)}
    </div>
    ${fileCard('Описание продукта.pdf', 'PDF · 4,2 МБ · v2', L.statusPill('done', 'Проверен', 'sm'), 'Загрузила Анна Волкова', 'Аналитик продаж +1')}
    ${fileCard('Сценарии продаж.docx', 'DOCX · 820 КБ · v1', L.statusPill('run', 'Проверяется', 'sm'), 'Загрузил Михаил Орлов', '')}
    ${fileCard('Коммерческое предложение.pdf', 'PDF · 1,4 МБ · v1', L.statusPill('done', 'Результат запуска', 'sm'), 'Запуск «Подготовить предложение»', 'Редактор предложений')}
    ${fileCard('Прайс-лист 2026.xlsx', 'XLSX · 260 КБ · v1', L.statusPill('err', 'Изолирован', 'sm'), 'Файл недоступен: замените или удалите', '')}
  </div>
`));

// ============ UX-14 mobile ============
const autoCard = (name, sched, tz, target, pill, next, last) => L.mCard(`
  <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 10px;">
    <span style="font-size: 15px; font-weight: 600; line-height: 1.3;">${name}</span>
    <button aria-label="Действия расписания" style="width: 44px; height: 44px; margin: -12px -12px 0 0; border: 0; background: none; color: ${T.mut}; display: flex; align-items: center; justify-content: center;">${L.icon('dots', 18)}</button>
  </div>
  <div style="margin-top: 8px;">${pill}</div>
  <div style="display: flex; align-items: center; gap: 8px; margin-top: 10px; font-size: 13px; color: ${T.ink2};">${L.icon("calendar", 16)}${sched} <span style="color: ${T.mut}; font-size: 11.5px;">${tz}</span></div>
  <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 7px;">Цель: ${target}</div>
  <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 10px; padding-top: 10px; border-top: 1px solid ${T.row}; font-size: 11.5px; color: ${T.mut};">
    <span>След.: <span style="color: ${T.sec};">${next}</span></span><span>${last}</span>
  </div>`);

export const m14 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Автоматизации', back: true, sub: 'Корпоративные продажи' })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">    ${L.mBtn('Создать расписание', 'pri')}
    ${autoCard('Ежедневная сводка по лидам', 'По будням в 09:00', 'Europe/Saratov', 'Аналитик продаж',
      L.statusPill('done', 'Активно', 'sm'), 'Пн, 24 августа, 09:00', 'Последний: успешно')}
    ${autoCard('Недельное предложение руководителю', 'Каждую пятницу в 16:00', 'Europe/Saratov', 'Процесс «Подготовка сводки»',
      L.statusPill('done', 'Активно', 'sm'), 'Пт, 28 августа, 16:00', 'Последний: успешно')}
    ${autoCard('Проверка новых договоров', 'Ежедневно в 07:30', 'Europe/Saratov', 'Юридический консультант',
      L.statusPill('off', 'Приостановлено', 'sm'), '—', 'Последний: успешно')}
    <div style="border: 1px solid ${T.warnLine}; border-radius: 12px; background: ${T.warnTint}; padding: 11px 12px; display: flex; align-items: flex-start; gap: 9px;">
      <span style="color: ${T.warn}; display: flex; padding-top: 1px;">${L.icon("alert", 15)}</span>
      <span style="font-size: 12px; color: ${T.sec}; line-height: 1.45;">Вчера уведомление не доставлено в Mattermost. Запуск выполнен успешно.</span>
    </div>
  </div>
`));
