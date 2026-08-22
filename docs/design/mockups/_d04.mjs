import * as L from './_lib.mjs';
const T = L.T;

const projCard = (name, desc, stats, state, changed) => `
  <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 18px; display: flex; flex-direction: column; gap: 12px;">
    <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 12px;">
      <div style="min-width: 0;">
        <div style="font-size: 16px; font-weight: 600; letter-spacing: -0.01em;">${name}</div>
        <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 4px; line-height: 1.45;">${desc}</div>
      </div>
      <span style="flex: 0 0 auto; color: ${T.mut}; display: flex;">${L.icon('dots', 16)}</span>
    </div>
    <div style="display: flex; align-items: center; gap: 16px; font-size: 12px; color: ${T.sec};">
      ${stats.map((s) => `<span style="display: flex; align-items: center; gap: 6px;"><span style="font-family: ${T.mono}; font-size: 13px; color: ${T.ink}; font-weight: 600;">${s[0]}</span>${s[1]}</span>`).join('')}
    </div>
    <div style="display: flex; align-items: center; justify-content: space-between; gap: 12px; padding-top: 12px; border-top: 1px solid ${T.row};">
      <span style="display: flex; align-items: center; gap: 10px;">${state}<span style="font-size: 11.5px; color: ${T.mut};">${changed}</span></span>
      ${L.btn('Открыть', 'sec', 30)}
    </div>
  </div>`;

// ============ UX-04 Проекты ============
export const d04 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Все проекты',
  body: `
  ${L.contentHead({
    path: ['Проекты'],
    title: 'Проекты',
    sub: 'Организуйте ИИ-сотрудников, процессы, файлы и запуски по направлениям работы',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Создать проект</button>`,
  })}

  <div style="flex: 1 1 auto; padding: 18px 24px 24px; display: flex; flex-direction: column; gap: 16px; background: ${T.subtle}; min-height: 0;">
    <div style="display: flex; align-items: center; gap: 10px;">
      ${L.searchBox('Найти проект', '320px')}
      <button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Состояние: Активные ${L.icon('chev', 13)}</button>
      <div style="flex: 1 1 auto;"></div>
      <span style="display: flex; align-items: center; border: 1px solid ${T.line}; border-radius: 8px; overflow: hidden;">
        <span aria-label="Плитками" style="width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; background: ${T.accTint}; color: ${T.accDark};">${L.icon('grid', 15)}</span>
        <span style="width: 1px; height: 32px; background: ${T.line};"></span>
        <span aria-label="Списком" style="width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; background: ${T.bg}; color: ${T.sec};">${L.icon('list', 15)}</span>
      </span>
    </div>

    <div style="display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px;">
      ${projCard('Корпоративные продажи', 'Подготовка предложений и аналитика клиентов',
        [['3', 'ИИ-сотрудника'], ['2', 'Процесса'], ['1', 'активный запуск']],
        L.statusPill('run', 'Идёт работа', 'sm'), 'изменён сегодня')}
      ${projCard('Клиентская поддержка', 'Разбор обращений и подготовка ответов',
        [['2', 'ИИ-сотрудника'], ['1', 'Процесс'], ['0', 'запусков']],
        L.statusPill('off', 'Нет активной работы', 'sm'), 'изменён вчера')}
      ${projCard('Релиз продукта', 'Подготовка материалов и согласование выпуска',
        [['2', 'ИИ-сотрудника'], ['1', 'Процесс'], ['1', 'решение']],
        L.statusPill('gate', 'Ожидает решения', 'sm'), 'изменён 20 августа')}
    </div>

    <div style="border: 1px dashed ${T.lineStrong}; border-radius: 12px; padding: 16px 18px; display: flex; align-items: center; gap: 12px; background: ${T.bg};">
      <span style="color: ${T.mut}; display: flex;">${L.icon('plus', 18)}</span>
      <span style="flex: 1 1 auto;">
        <span style="display: block; font-size: 13px; font-weight: 500;">Новый Проект создаётся за один шаг</span>
        <span style="display: block; font-size: 12px; color: ${T.mut}; margin-top: 2px;">Понадобятся только название, краткое описание работы и язык. Интеграции подключаются позже и не обязательны.</span>
      </span>
      ${L.btn('Попросить помощника', 'sec', 32)}
    </div>
  </div>`,
}));
