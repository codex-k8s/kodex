import * as L from './_lib.mjs';
const T = L.T;

const evCard = (time, actor, action, outcome) => L.mCard(`
  <div style="display: flex; align-items: flex-start; justify-content: space-between; gap: 10px;">
    <span style="font-family: ${T.mono}; font-size: 11.5px; color: ${T.mut};">${time}</span>
    ${outcome}
  </div>
  <div style="font-size: 13.5px; color: ${T.ink}; margin-top: 9px; line-height: 1.45;">${action}</div>
  <div style="font-size: 12px; color: ${T.mut}; margin-top: 7px; line-height: 1.45;">${actor}</div>`);

// ============ UX-19 mobile ============
export const m19 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Аудит и диагностика', back: true, sub: 'Администрирование' })}
  ${L.mTabs(['Аудит', 'Диагностика'], 'Аудит')}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 10px; background: ${T.subtle}; overflow: hidden;">
    <div style="display: flex; align-items: center; gap: 8px;">
      <button style="height: 40px; display: flex; align-items: center; gap: 8px; padding: 0 13px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 13px;">${L.icon('filter', 15)}Фильтры<span style="min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: ${T.acc}; color: #FFFFFF; font-size: 10.5px; font-weight: 600; display: flex; align-items: center; justify-content: center;">1</span></button>
      ${L.chip('7 дней', true)}
    </div>

    <div style="font-size: 11.5px; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.mut}; padding: 2px 2px 0;">Сегодня, 22 августа</div>

    ${evCard('10:12', 'Анна Волкова через Помощника Kodex', 'Создан Проект «Корпоративные продажи»', L.statusPill('done', 'Успешно', 'sm'))}
    ${evCard('10:13', 'Помощник Kodex от имени Анны Волковой', 'Создан ИИ-сотрудник «Аналитик продаж»', L.statusPill('done', 'Успешно', 'sm'))}
    ${evCard('10:41', 'Михаил Орлов', 'Запущен Процесс «Обработка нового лида»', L.statusPill('done', 'Успешно', 'sm'))}
    ${evCard('11:04', 'Система · код <span style="font-family: ' + T.mono + '; color: ' + T.err + ';">RUNTIME_EXECUTION_FAILED</span>', 'Запуск «Проверить условия договора» остановлен', L.statusPill('err', 'Ошибка', 'sm'))}
    ${evCard('11:05', 'Доставка уведомления · основной запуск завершён успешно', 'Уведомление Mattermost не доставлено', L.statusPill('wait', 'Доставка', 'sm'))}
  </div>
`));
