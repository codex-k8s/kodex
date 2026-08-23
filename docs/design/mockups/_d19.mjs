import * as L from './_lib.mjs';
const T = L.T;

const tabs = (items, active, badge) => `
  <div style="display: flex; align-items: stretch; gap: 4px; padding: 0 24px; border-bottom: 1px solid ${T.line}; background: ${T.bg}; flex: 0 0 42px;">
    ${items.map((t) => `<span style="display: flex; align-items: center; gap: 7px; padding: 0 12px; font-size: 12.5px; ${t === active ? `color: ${T.ink}; font-weight: 600; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${t}${t === 'Диагностика' ? badge : ''}</span>`).join('')}
  </div>`;

const ev = (time, actor, action, outcome, sel) => `
  <div style="display: flex; align-items: flex-start; gap: 12px; padding: 12px 16px; border-top: 1px solid ${T.row}; ${sel ? `background: ${T.accTint}; box-shadow: inset 2px 0 0 ${T.acc};` : ''}">
    <span style="flex: 0 0 46px; font-family: ${T.mono}; font-size: 11.5px; color: ${T.mut}; padding-top: 1px;">${time}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 12.5px; color: ${T.ink}; line-height: 1.45; font-weight: ${sel ? 500 : 400};">${action}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 3px;">${actor}</span>
    </span>
    <span style="flex: 0 0 auto;">${outcome}</span>
  </div>`;

const kv = (k, v) => `<div style="display: flex; gap: 12px; padding: 9px 0; border-top: 1px solid ${T.row};"><span style="flex: 0 0 152px; font-size: 12px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${v}</span></div>`;

// ============ UX-19 Аудит и диагностика ============
export const d19 = L.page(L.shellDesktop({
  nav: 'admin', project: 'Все проекты',
  body: `
  ${L.contentHead({
    path: ['Администрирование', 'Аудит и диагностика'],
    title: 'Аудит и диагностика',
    sub: 'Проверяйте изменения, ошибки и действия пользователей',
    actions: `${L.btn('Экспорт', 'sec', 36)}`,
  })}
  ${tabs(['Аудит', 'Диагностика'], 'Аудит', `<span style="min-width: 18px; height: 18px; padding: 0 6px; border-radius: 9px; background: ${T.warn}; color: #FFFFFF; font-size: 10.5px; font-weight: 600; display: flex; align-items: center; justify-content: center;">1</span>`)}

  <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 12px; padding: 14px 24px 22px; background: ${T.subtle}; min-height: 0;">

    <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
      ${L.searchBox('Найти по имени ресурса', '260px')}
      ${['Проект: Все', 'Кто: Все', 'Категория: Все действия', 'Результат: Все', 'Период: 7 дней'].map((f) => `<button style="display: flex; align-items: center; gap: 7px; height: 32px; padding: 0 11px; border-radius: 8px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">${f} ${L.icon('chev', 13)}</button>`).join('')}
    </div>

    <div style="flex: 1 1 auto; display: flex; gap: 16px; min-height: 0;">

      <div style="flex: 1 1 auto; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; overflow: hidden; display: flex; flex-direction: column; min-width: 0;">
        <div style="padding: 11px 16px; background: ${T.field}; font-size: 11px; letter-spacing: 0.04em; text-transform: uppercase; color: ${T.mut};">Сегодня, 22 августа</div>
        ${ev('10:12', 'Анна Волкова через Помощника MatterCodex',
          'Создан Проект «Корпоративные продажи»', L.statusPill('done', 'Успешно', 'sm'), false)}
        ${ev('10:13', 'Помощник MatterCodex от имени Анны Волковой',
          'Создан ИИ-сотрудник «Аналитик продаж»', L.statusPill('done', 'Успешно', 'sm'), true)}
        ${ev('10:41', 'Михаил Орлов',
          'Запущен Процесс «Обработка нового лида»', L.statusPill('done', 'Успешно', 'sm'), false)}
        ${ev('11:04', 'Система',
          'Запуск «Проверить условия договора» остановлен · код <span style="font-family: ' + T.mono + '; color: ' + T.err + ';">RUNTIME_EXECUTION_FAILED</span>', L.statusPill('err', 'Ошибка', 'sm'), false)}
        ${ev('11:05', 'Доставка уведомления',
          'Уведомление Mattermost не доставлено · основной запуск завершён успешно', L.statusPill('wait', 'Доставка', 'sm'), false)}
        ${ev('11:32', 'Анна Волкова',
          'Опубликованы инструкции ИИ-сотрудника «Аналитик продаж» · v3', L.statusPill('done', 'Успешно', 'sm'), false)}
        <div style="flex: 1 1 auto;"></div>
        <div style="padding: 11px 16px; border-top: 1px solid ${T.row}; background: ${T.subtle}; font-size: 11.5px; color: ${T.mut};">
          Новые события добавляются автоматически и не сбивают текущий выбор.
        </div>
      </div>

      <aside style="flex: 0 0 400px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; display: flex; flex-direction: column; overflow: hidden;">
        <div style="padding: 14px 16px; border-bottom: 1px solid ${T.line};">
          <div style="font-size: 14px; font-weight: 600; line-height: 1.35;">Создан ИИ-сотрудник «Аналитик продаж»</div>
          <div style="margin-top: 8px;">${L.statusPill('done', 'Успешно', 'sm')}</div>
        </div>
        <div style="flex: 1 1 auto; padding: 14px 16px; display: flex; flex-direction: column; gap: 14px; min-height: 0;">
          <div>
            ${kv('Инициатор', 'Анна Волкова')}
            ${kv('Исполнитель команды', 'Помощник MatterCodex')}
            ${kv('Источник', 'Системный помощник')}
            ${kv('Проект', 'Корпоративные продажи')}
            ${kv('Тип операции', 'Создание ИИ-сотрудника')}
            ${kv('Время', '22 августа, 10:13')}
          </div>

          <div style="border: 1px solid ${T.line}; border-radius: 10px; background: ${T.subtle}; padding: 12px 13px;">
            <div style="font-size: 11.5px; letter-spacing: 0.03em; text-transform: uppercase; color: ${T.mut};">Было → стало</div>
            <div style="display: flex; align-items: center; gap: 10px; margin-top: 8px; font-size: 12.5px;">
              <span style="color: ${T.sec};">ресурс отсутствовал</span>
              <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 14)}</span>
              <span style="color: ${T.ink}; font-weight: 500;">Аналитик продаж создан</span>
            </div>
          </div>

          <div style="display: flex; align-items: center; gap: 10px; padding: 11px 12px; border-radius: 9px; border: 1px solid ${T.line};">
            <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 15)}</span>
            <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.sec};">Технические подробности</span>
            <span style="font-size: 11px; color: ${T.mut};">скрыто политикой безопасности</span>
          </div>
        </div>
        <div style="padding: 12px 16px 16px; border-top: 1px solid ${T.line}; display: flex; gap: 8px;">
          <button style="flex: 1 1 0; height: 34px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 12.5px; font-weight: 600;">Открыть ИИ-сотрудника</button>
          <button style="flex: 1 1 0; height: 34px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Открыть диалог помощника</button>
        </div>
      </aside>
    </div>
  </div>`,
}));
