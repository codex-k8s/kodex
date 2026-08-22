import * as L from './_lib.mjs';
const T = L.T;

const m = (v, l) => `<div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 10px; background: ${T.bg}; padding: 9px 10px;"><div style="font-size: 17px; font-weight: 600; letter-spacing: -0.02em;">${v}</div><div style="font-size: 10.5px; color: ${T.mut}; margin-top: 1px;">${l}</div></div>`;

// ============ UX-03 mobile ============
export const m03 = L.page(L.frameMobile(`
  <header style="flex: 0 0 56px; display: flex; align-items: center; gap: 6px; padding: 0 16px; border-bottom: 1px solid ${T.line};">
    <button aria-label="Меню" style="width: 44px; height: 44px; margin-left: -10px; border: 0; background: none; color: ${T.ink}; display: flex; align-items: center; justify-content: center;">${L.icon('menu', 22)}</button>
    <button style="flex: 1 1 auto; min-width: 0; height: 36px; display: flex; align-items: center; gap: 7px; padding: 0 10px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13px;">
      <span style="width: 6px; height: 6px; border-radius: 3px; background: ${T.acc};"></span>Все проекты<span style="margin-left: auto; color: ${T.mut}; display: flex;">${L.icon('chev', 15)}</span>
    </button>
    <span style="position: relative; display: flex; align-items: center; justify-content: center; width: 44px; height: 44px; color: ${T.sec};">${L.icon('decision', 20)}
      <span style="position: absolute; top: 6px; right: 4px; min-width: 16px; height: 16px; padding: 0 4px; border-radius: 8px; background: ${T.warn}; color: #FFFFFF; font-size: 10px; font-weight: 700; display: flex; align-items: center; justify-content: center;">1</span>
    </span>
    ${L.avatar('АВ', 30)}
  </header>

  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">
    <div>
      <h1 style="margin: 0; font-size: 21px; font-weight: 600; letter-spacing: -0.02em;">Добрый день, Анна</h1>
      <p style="margin: 5px 0 0; font-size: 13.5px; color: ${T.sec};">Вот что происходит с вашими ИИ-сотрудниками</p>
    </div>
    ${L.mBtn('Запустить работу', 'pri')}

    <div style="display: flex; gap: 8px;">${m("3", "проекта")}${m("7", "агентов")}${m("2", "запуска")}${m("1", "решение")}</div>

    ${L.mCard(`
      <div style="display: flex; align-items: center; gap: 8px; font-size: 11.5px; font-weight: 600; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.warn};">${L.icon('shield', 14)}Требует решения</div>
      <div style="font-size: 15px; font-weight: 600; margin-top: 10px;">Согласовать коммерческое предложение</div>
      <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 4px;">Процесс ждёт вашего решения · сегодня, 18:00</div>
      <div style="margin-top: 12px;"><button style="width: 100%; height: 44px; border-radius: 10px; border: 0; background: ${T.warn}; color: #FFFFFF; font: inherit; font-size: 14px; font-weight: 600;">Рассмотреть</button></div>`,
      `border-color: ${T.warnLine}; background: ${T.warnTint};`)}

    ${L.mSectionTitle('Текущая работа', `<a href="#" style="font-size: 12.5px;">Все</a>`)}
    ${L.mCard(`
      <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 10px;">
        <span style="font-size: 14px; font-weight: 600; line-height: 1.35;">Подготовить коммерческое предложение</span>
        ${L.statusPill('run', 'Работает', 'sm')}
      </div>
      <div style="font-size: 12px; color: ${T.mut}; margin-top: 6px;">Корпоративные продажи · Аналитик продаж</div>
      <div style="font-size: 12.5px; color: ${T.ink2}; margin-top: 8px; line-height: 1.45;">Сравниваю требования клиента с загруженными материалами</div>
      <div style="font-family: ${T.mono}; font-size: 11.5px; color: ${T.mut}; margin-top: 8px;">12 мин</div>`)}
    ${L.mCard(`
      <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 10px;">
        <span style="font-size: 14px; font-weight: 600; line-height: 1.35;">Разобрать обращения за неделю</span>
        ${L.statusPill('wait', 'В очереди', 'sm')}
      </div>
      <div style="font-size: 12px; color: ${T.mut}; margin-top: 6px;">Клиентская поддержка</div>`)}

    ${L.mSectionTitle('Последние результаты')}
    ${L.mCard(`
      <div style="display: flex; align-items: center; gap: 11px;">
        <span style="color: ${T.sec}; display: flex;">${L.icon('file', 18)}</span>
        <span style="flex: 1 1 auto; min-width: 0;">
          <span style="display: block; font-size: 13.5px; font-weight: 500;">Отчёт по обращениям.pdf</span>
          <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">Клиентская поддержка · вчера</span>
        </span>
        <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 16)}</span>
      </div>`)}
  </div>
`));

// ============ UX-04 mobile ============
const projCard = (name, desc, stats, pill) => L.mCard(`
  <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 10px;">
    <span style="font-size: 16px; font-weight: 600; line-height: 1.3;">${name}</span>
    <span style="color: ${T.mut}; display: flex; padding-top: 2px;">${L.icon('dots', 18)}</span>
  </div>
  <div style="font-size: 13px; color: ${T.sec}; margin-top: 5px; line-height: 1.45;">${desc}</div>
  <div style="display: flex; align-items: center; gap: 14px; margin-top: 11px; font-size: 12px; color: ${T.sec};">
    ${stats.map((s) => `<span><span style="font-family: ${T.mono}; font-size: 13px; color: ${T.ink}; font-weight: 600;">${s[0]}</span> ${s[1]}</span>`).join('')}
  </div>
  <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 12px; padding-top: 12px; border-top: 1px solid ${T.row};">
    ${pill}
    <button style="height: 44px; padding: 0 18px; border-radius: 10px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13.5px; font-weight: 500;">Открыть</button>
  </div>`);

export const m04 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Проекты' })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">
    <p style="margin: 0; font-size: 13.5px; color: ${T.sec}; line-height: 1.5;">Организуйте ИИ-сотрудников, процессы, файлы и запуски по направлениям работы</p>
    ${L.mBtn('Создать проект', 'pri')}
    <div style="display: flex; gap: 8px;">
      ${L.chip('Активные', true)}${L.chip('Все')}${L.chip('Архив')}
    </div>
    ${projCard('Корпоративные продажи', 'Подготовка предложений и аналитика клиентов',
      [['3', 'агента'], ['2', 'Процесса'], ['1', 'запуск']], L.statusPill('run', 'Идёт работа', 'sm'))}
    ${projCard('Клиентская поддержка', 'Разбор обращений и подготовка ответов',
      [['2', 'агента'], ['1', 'Процесс'], ['0', 'запусков']], L.statusPill('off', 'Нет активной работы', 'sm'))}
    ${projCard('Релиз продукта', 'Подготовка материалов и согласование выпуска',
      [['2', 'агента'], ['1', 'Процесс'], ['1', 'решение']], L.statusPill('gate', 'Ожидает решения', 'sm'))}
  </div>
`));
