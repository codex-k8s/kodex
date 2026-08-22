import * as L from './_lib.mjs';
const T = L.T;

const wfCard = (name, purpose, pill, coord, parts, gates, changed, lastRun, actions) => `
  <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px; display: flex; align-items: flex-start; gap: 16px;">
    <span style="width: 38px; height: 38px; flex: 0 0 38px; border-radius: 10px; background: ${T.field}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('wf', 20)}</span>
    <div style="flex: 1 1 auto; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 10px;">
        <span style="font-size: 15px; font-weight: 600;">${name}</span>${pill}
      </div>
      <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 4px; line-height: 1.45;">${purpose}</div>
      <div style="display: flex; align-items: center; gap: 16px; margin-top: 10px; font-size: 12px; color: ${T.mut};">
        <span>Координатор: <span style="color: ${T.sec};">${coord}</span></span>
        <span>Участники: <span style="color: ${T.sec};">${parts}</span></span>
        <span>Решения человека: <span style="color: ${T.sec};">${gates}</span></span>
        <span>Изменён: <span style="color: ${T.sec};">${changed}</span></span>
      </div>
    </div>
    <div style="flex: 0 0 250px; display: flex; flex-direction: column; align-items: flex-end; gap: 10px;">
      <span style="font-size: 11.5px; color: ${T.mut};">${lastRun}</span>
      <span style="display: flex; gap: 8px;">${actions}</span>
    </div>
  </div>`;

// ============ UX-08 Процессы ============
export const d08 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.projectNav('workflows')}
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'Процессы'],
    title: 'Процессы',
    sub: 'Организуйте повторяемую работу нескольких ИИ-сотрудников',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Создать Процесс</button>`,
  })}

  <div style="flex: 1 1 auto; padding: 18px 24px 24px; display: flex; flex-direction: column; gap: 14px; background: ${T.subtle}; min-height: 0;">
    <div style="display: flex; align-items: center; gap: 10px;">
      ${L.searchBox('Найти Процесс', '320px')}
      <button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Все состояния ${L.icon('chev', 13)}</button>
      <button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Все координаторы ${L.icon('chev', 13)}</button>
    </div>

    ${wfCard('Обработка нового лида',
      'Исследовать компанию, подготовить предложение и получить решение владельца',
      L.statusPill('done', 'Опубликован · v3', 'sm'),
      'Менеджер продаж', 'Аналитик продаж +2', '1', 'сегодня',
      'Последний запуск: выполняется',
      `${L.btn('Открыть', 'sec', 30)}${L.btn('Запустить', 'pri', 30)}`)}

    ${wfCard('Подготовка ответа клиенту',
      'Собрать контекст обращения и подготовить ответ на согласование',
      L.statusPill('done', 'Опубликован · v1', 'sm'),
      'Менеджер продаж', '2 ИИ-сотрудника', 'нет', '19 августа',
      'Последний запуск: успешно',
      `${L.btn('Открыть', 'sec', 30)}${L.btn('Запустить', 'pri', 30)}`)}

    ${wfCard('Согласование договора',
      'Проверить условия, оценить риски и провести два согласования',
      L.statusPill('wait', 'Черновик · есть 2 замечания', 'sm'),
      'Менеджер продаж', '3 ИИ-сотрудника', '2', 'вчера',
      'Запусков не было',
      `${L.btn('Открыть', 'sec', 30)}${L.btn('Запустить', 'off', 30)}`)}

    <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 16px; display: flex; align-items: flex-start; gap: 10px;">
      <span style="color: ${T.mut}; display: flex; padding-top: 1px;">${L.icon('alert', 15)}</span>
      <span style="font-size: 12.5px; color: ${T.sec}; line-height: 1.5;">
        «Согласование договора» нельзя запустить, пока он в черновике. Состояние определения Процесса не смешивается с результатом последнего запуска.
      </span>
    </div>

    <div style="flex: 1 1 auto;"></div>

    <div style="display: flex; align-items: center; gap: 12px; border: 1px dashed ${T.lineStrong}; border-radius: 12px; background: ${T.bg}; padding: 16px 18px;">
      <span style="color: ${T.mut}; display: flex;">${L.icon('plus', 18)}</span>
      <span style="flex: 1 1 auto;">
        <span style="display: block; font-size: 13px; font-weight: 500;">Процесс собирается пошагово: «Основное», «Участники», «Шаги», «Проверка»</span>
        <span style="display: block; font-size: 12px; color: ${T.mut}; margin-top: 2px;">Внешние интеграции на этом пути не требуются.</span>
      </span>
      ${L.btn('Собрать с помощником', 'sec', 32)}
    </div>
  </div>`,
}));
