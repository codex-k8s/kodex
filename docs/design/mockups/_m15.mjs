import * as L from './_lib.mjs';
const T = L.T;

const intRow = (name, caps, pill) => L.mCard(`
  <div style="display: flex; align-items: center; gap: 11px;">
    <span style="width: 34px; height: 34px; flex: 0 0 34px; border-radius: 9px; background: ${T.field}; border: 1px solid ${T.line}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('plug', 17)}</span>
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 14.5px; font-weight: 600;">${name}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 3px; line-height: 1.4;">${caps}</span>
    </span>
    <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 18)}</span>
  </div>
  <div style="margin-top: 10px;">${pill}</div>`);

// ============ UX-15 mobile ============
export const m15 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Интеграции' })}
  ${L.mTabs(['Подключения', 'Каталог'], 'Каталог')}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 10px; background: ${T.subtle}; overflow: hidden;">
    ${L.mBtn('Подключить интеграцию', 'pri')}
    <div style="display: flex; align-items: center; gap: 8px;">
      <button style="height: 38px; display: flex; align-items: center; gap: 7px; padding: 0 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">${L.icon('filter', 15)}Категории</button>
      ${L.chip('Все', true)}${L.chip('Коммуникации')}
    </div>
    ${intRow('CRM', 'Читать клиентов · Создавать заметки · Обновлять этап сделки', L.statusPill('done', 'Подключено', 'sm'))}
    ${intRow('Mattermost', 'Входящие сообщения · Уведомления · Зеркало результатов · Решения человека', L.statusPill('off', 'Отключено · необязательно', 'sm'))}
    ${intRow('Электронная почта', 'Отправлять письма · Читать входящие', L.statusPill('off', 'Не подключено', 'sm'))}
    ${intRow('Облачное хранилище', 'Читать файлы · Сохранять результаты', L.statusPill('off', 'Не подключено', 'sm'))}
    <div style="border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 12px; display: flex; align-items: flex-start; gap: 9px;">
      <span style="color: ${T.ok}; display: flex; padding-top: 1px;">${L.icon('check', 16)}</span>
      <span style="font-size: 12.5px; color: ${T.ink2}; line-height: 1.45;">Control Center и выполнение задач работают без Mattermost и любых других подключений.</span>
    </div>
  </div>
`));

// ============ UX-16 mobile ============
export const m16 = L.page(L.frameMobile(`
  ${L.mTop({ title: 'Решения', sub: '2 ожидают вашего ответа' })}
  <div style="flex: 1 1 auto; padding: 14px; display: flex; flex-direction: column; gap: 11px; background: ${T.subtle}; overflow: hidden;">
    <div style="display: flex; align-items: center; gap: 8px;">
      <button style="height: 38px; display: flex; align-items: center; gap: 7px; padding: 0 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">${L.icon('filter', 15)}Фильтры</button>
      ${L.chip('Ожидают меня', true)}${L.chip('Срочные')}
    </div>

    ${L.mCard(`
      <div style="font-size: 15.5px; font-weight: 600; line-height: 1.3;">Согласовать коммерческое предложение</div>
      <div style="margin-top: 10px;">${L.statusPill('gate', 'Ожидает вашего решения', 'sm')}</div>
      <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 10px; line-height: 1.5;">Корпоративные продажи · запуск «Подготовить предложение для компании Север»</div>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 8px; font-size: 12px; color: ${T.mut};">
        <span>Запросил: Менеджер продаж</span><span>·</span><span>8 минут назад</span>
      </div>
      <div style="margin-top: 12px;"><button style="width: 100%; height: 46px; border-radius: 10px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 15px; font-weight: 600;">Открыть решение</button></div>`,
      `border-color: ${T.warnLine};`)}

    ${L.mCard(`
      <div style="font-size: 15.5px; font-weight: 600; line-height: 1.3;">Разрешить обновление этапа сделки в CRM</div>
      <div style="margin-top: 10px;">${L.statusPill('wait', 'Изменение во внешней системе', 'sm')}</div>
      <div style="font-size: 12.5px; color: ${T.sec}; margin-top: 10px; line-height: 1.5;">Корпоративные продажи · запуск «Обработка нового лида»</div>
      <div style="display: flex; align-items: center; gap: 8px; margin-top: 8px; font-size: 12px; color: ${T.mut};"><span>Срок: сегодня, 18:00</span></div>
      <div style="margin-top: 12px;">${L.mBtn('Открыть решение', 'sec')}</div>`)}

    <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 12px; display: flex; align-items: flex-start; gap: 9px;">
      <span style="color: ${T.mut}; display: flex; padding-top: 1px;">${L.icon('alert', 15)}</span>
      <span style="font-size: 12.5px; color: ${T.sec}; line-height: 1.45;">Решение принимается один раз. Если его уже принял другой участник, вы увидите фактический результат и ссылку на запуск.</span>
    </div>
  </div>
`));
