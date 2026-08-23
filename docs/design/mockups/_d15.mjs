import * as L from './_lib.mjs';
const T = L.T;

const tabs = (items, active) => `
  <div style="display: flex; gap: 4px; padding: 0 24px; border-bottom: 1px solid ${T.line}; background: ${T.bg}; flex: 0 0 42px; align-items: stretch;">
    ${items.map((t) => `<span style="display: flex; align-items: center; padding: 0 12px; font-size: 12.5px; ${t === active ? `color: ${T.ink}; font-weight: 600; box-shadow: inset 0 -2px 0 ${T.acc};` : `color: ${T.sec};`}">${t}</span>`).join('')}
  </div>`;

const defCard = (name, purpose, caps, state, action, sel) => `
  <div style="border: 1px solid ${sel ? T.acc : T.line}; ${sel ? `box-shadow: 0 0 0 2px ${T.accTint};` : ''} border-radius: 12px; background: ${T.bg}; padding: 14px; display: flex; flex-direction: column; gap: 10px;">
    <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px;">
      <span style="display: flex; align-items: center; gap: 10px;">
        <span style="width: 30px; height: 30px; border-radius: 8px; background: ${T.field}; border: 1px solid ${T.line}; color: ${T.sec}; display: flex; align-items: center; justify-content: center;">${L.icon('plug', 16)}</span>
        <span style="font-size: 13.5px; font-weight: 600;">${name}</span>
      </span>
      ${state}
    </div>
    <div style="font-size: 12px; color: ${T.sec}; line-height: 1.45; min-height: 34px;">${purpose}</div>
    <div style="display: flex; flex-wrap: wrap; gap: 5px;">
      ${caps.map((c) => `<span style="display: inline-flex; align-items: center; height: 22px; padding: 0 8px; border-radius: 6px; background: ${T.field}; border: 1px solid ${T.line}; font-size: 11px; color: ${T.sec};">${c}</span>`).join('')}
    </div>
    <div style="display: flex; justify-content: flex-end; padding-top: 4px;">${action}</div>
  </div>`;

const capRow = (name, risk, st) => `
  <div style="display: flex; align-items: center; gap: 12px; padding: 10px 0; border-top: 1px solid ${T.row};">
    <span style="flex: 1 1 auto; min-width: 0;">
      <span style="display: block; font-size: 12.5px; font-weight: 500;">${name}</span>
      <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">${risk}</span>
    </span>
    ${st}
  </div>`;

// ============ UX-15 Интеграции ============
export const d15 = L.page(L.shellDesktop({
  nav: 'integrations', project: 'Все проекты',
  body: `
  ${L.contentHead({
    path: ['Интеграции'],
    title: 'Интеграции',
    sub: 'Подключайте внешние системы только там, где они нужны',
    actions: `<button style="height: 36px; padding: 0 18px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 13.5px; font-weight: 600;">Подключить интеграцию</button>`,
  })}
  ${tabs(['Подключения', 'Каталог'], 'Каталог')}

  <div style="flex: 1 1 auto; display: flex; gap: 18px; padding: 16px 24px 22px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 14px; min-width: 0;">
      <div style="display: flex; align-items: center; gap: 8px;">
        ${L.searchBox('Найти интеграцию', '260px')}
        ${['Все', 'Коммуникации', 'Данные', 'Документы', 'Разработка', 'Инфраструктура'].map((c, i) => L.chip(c, i === 0)).join('')}
      </div>

      <div style="display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px;">
        ${defCard('CRM', 'Чтение клиентов и запись разрешённых результатов работы',
          ['Читать клиентов', 'Создавать заметки', 'Обновлять этап сделки'],
          L.statusPill('done', 'Подключено', 'sm'), L.btn('Открыть', 'sec', 30), true)}
        ${defCard('Электронная почта', 'Отправка подготовленных писем и приём входящих обращений',
          ['Отправлять письма', 'Читать входящие'],
          L.statusPill('off', 'Не подключено', 'sm'), L.btn('Подключить', 'sec', 30), false)}
        ${defCard('Облачное хранилище', 'Чтение файлов из внешнего хранилища и сохранение результатов',
          ['Читать файлы', 'Сохранять результаты'],
          L.statusPill('off', 'Не подключено', 'sm'), L.btn('Подключить', 'sec', 30), false)}
        ${defCard('GitHub', 'Чтение репозиториев и создание разрешённых изменений',
          ['Читать репозитории', 'Создавать pull request'],
          L.statusPill('off', 'Не подключено', 'sm'), L.btn('Подключить', 'sec', 30), false)}
        ${defCard('Mattermost', 'Обмен сообщениями, уведомления и зеркалирование результатов',
          ['Входящие сообщения', 'Уведомления', 'Зеркало результатов', 'Решения человека'],
          L.statusPill('off', 'Отключено · необязательно', 'sm'), L.btn('Открыть', 'sec', 30), false)}
        ${defCard('Kubernetes', 'Запуск задач в выделенном вычислительном контуре',
          ['Запускать задачи', 'Читать состояние'],
          L.statusPill('off', 'Не подключено', 'sm'), L.btn('Подключить', 'sec', 30), false)}
      </div>

      <div style="border: 1px solid ${T.okLine}; border-radius: 12px; background: ${T.okSoft}; padding: 14px 16px; display: flex; align-items: center; gap: 11px;">
        <span style="color: ${T.ok}; display: flex;">${L.icon('check', 17)}</span>
        <span style="font-size: 12.5px; color: ${T.ink2};">Платформа готова к работе с нулём подключений. Интеграции только расширяют возможности ИИ-сотрудников.</span>
      </div>
    </div>

    <aside style="flex: 0 0 388px; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; display: flex; flex-direction: column; overflow: hidden;">
      <div style="padding: 14px 16px; border-bottom: 1px solid ${T.line};">
        <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px;">
          <span style="font-size: 15px; font-weight: 600;">CRM · Основное</span>
          ${L.statusPill('done', 'Подключено', 'sm')}
        </div>
        <div style="display: flex; align-items: center; gap: 8px; margin-top: 8px; font-size: 11.5px; color: ${T.mut};">
          <span>Проверка: сегодня, 10:42 · успешно</span>
        </div>
      </div>

      <div style="flex: 1 1 auto; padding: 14px 16px; display: flex; flex-direction: column; gap: 14px; min-height: 0;">
        <div style="display: flex; align-items: center; gap: 10px; padding: 11px 12px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.subtle};">
          <span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">Учётные данные настроены · значение скрыто</span>
          ${L.btn('Проверить', 'sec', 30)}
        </div>

        <div>
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Возможности</div>
          ${capRow('Читать клиентов', 'Только чтение · без подтверждения', L.statusPill('done', 'Доступна', 'sm'))}
          ${capRow('Создавать заметки', 'Запись · без подтверждения', L.statusPill('done', 'Доступна', 'sm'))}
          ${capRow('Обновлять этап сделки', 'Запись · требует решения человека', L.statusPill('gate', 'С подтверждением', 'sm'))}
        </div>

        <div>
          <div style="display: flex; align-items: center; justify-content: space-between;">
            <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Разрешения</div>
            <a href="#" style="font-size: 12px;">Настроить</a>
          </div>
          ${capRow('Аналитик продаж', 'ИИ-сотрудник · Корпоративные продажи', `<span style="font-size: 11.5px; color: ${T.sec};">только чтение</span>`)}
          ${capRow('Обработка нового лида', 'Процесс · Корпоративные продажи', `<span style="font-size: 11.5px; color: ${T.sec};">чтение и заметки</span>`)}
        </div>
      </div>

      <div style="padding: 12px 16px 16px; border-top: 1px solid ${T.line};">
        <button style="width: 100%; height: 34px; border-radius: 9px; border: 1px solid ${T.errLine}; background: ${T.bg}; color: ${T.err}; font: inherit; font-size: 12.5px; font-weight: 500;">Отключить интеграцию</button>
        <p style="margin: 9px 0 0; font-size: 11px; color: ${T.mut}; line-height: 1.5;">Отключение потребует подтверждения и оставит выданные разрешения без действия.</p>
      </div>
    </aside>
  </div>`,
}));
