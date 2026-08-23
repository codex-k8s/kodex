import * as L from './_lib.mjs';
const T = L.T;

const seg = (items, active) => `
  <div style="display: inline-flex; padding: 3px; border-radius: 9px; background: ${T.field}; border: 1px solid ${T.line};">
    ${items.map((t) => `<span style="height: 30px; display: flex; align-items: center; padding: 0 16px; border-radius: 7px; font-size: 12.5px; ${t === active ? `background: ${T.bg}; color: ${T.ink}; font-weight: 600; box-shadow: 0 1px 2px rgba(16,22,30,0.08);` : `color: ${T.sec};`}">${t}</span>`).join('')}
  </div>`;

const sumRow = (k, v) => `<div style="display: flex; gap: 12px; padding: 9px 0; border-top: 1px solid ${T.row};"><span style="flex: 0 0 116px; font-size: 12px; color: ${T.mut};">${k}</span><span style="flex: 1 1 auto; font-size: 12.5px; color: ${T.ink2};">${v}</span></div>`;

// ============ UX-10 Новый запуск ============
export const d10 = L.page(L.shellDesktop({
  nav: 'projects', project: 'Корпоративные продажи',
  body: `
  ${L.contentHead({
    path: ['Проекты', 'Корпоративные продажи', 'Новый запуск'],
    title: 'Новый запуск',
    sub: 'Поставьте задачу ИИ-сотруднику или запустите готовый Процесс',
  })}

  <div style="flex: 1 1 auto; display: flex; gap: 20px; padding: 18px 24px 18px; background: ${T.subtle}; min-height: 0;">

    <div style="flex: 1 1 auto; display: flex; flex-direction: column; gap: 16px; min-width: 0;">
      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 18px; display: flex; flex-direction: column; gap: 14px;">
        <div style="display: flex; align-items: center; gap: 14px;">
          <span style="font-size: 12.5px; color: ${T.sec};">Что запускаем</span>
          ${seg(['ИИ-сотрудник', 'Процесс'], 'Процесс')}
        </div>
        ${L.field('Процесс', 'Обработка нового лида · опубликован v3')}
        ${L.textarea("Задача", "Подготовить коммерческое предложение для компании Север к встрече 27 августа", 52)}
      </div>

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 18px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Входные данные Процесса</div>
        <div style="display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 13px;">
          ${L.field('Компания', 'Север')}
          ${L.field('Контакт', 'Мария Соколова')}
          ${L.field('Цель встречи', 'Обсудить пилот на 50 сотрудников')}
          ${L.field('Срок', '27 августа, 16:00')}
        </div>
      </div>

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 18px;">
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Файлы</div>
          ${L.btn('Добавить файл', 'sec', 30)}
        </div>
        <div style="display: flex; align-items: center; gap: 12px; margin-top: 13px; padding: 11px 13px; border-radius: 9px; border: 1px solid ${T.okLine}; background: ${T.okSoft};">
          <span style="color: ${T.sec}; display: flex;">${L.icon('file', 18)}</span>
          <span style="flex: 1 1 auto;">
            <span style="display: block; font-size: 13px; font-weight: 500;">brief.pdf</span>
            <span style="display: block; font-size: 11.5px; color: ${T.mut}; margin-top: 2px;">1,8 МБ</span>
          </span>
          ${L.statusPill('done', 'Проверен и готов', 'sm')}
        </div>
      </div>

      <div style="display: flex; gap: 16px;">
        <div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Продолжение работы</div>
          <div style="display: flex; flex-direction: column; gap: 9px; margin-top: 12px;">
            <span style="display: flex; align-items: center; gap: 9px; font-size: 13px;"><span style="width: 16px; height: 16px; border-radius: 8px; border: 5px solid ${T.acc}; box-sizing: border-box;"></span>Новая сессия</span>
            <span style="display: flex; align-items: center; gap: 9px; font-size: 13px; color: ${T.sec};"><span style="width: 16px; height: 16px; border-radius: 8px; border: 1px solid ${T.lineStrong}; box-sizing: border-box;"></span>Продолжить существующую</span>
          </div>
        </div>
        <div style="flex: 1 1 0; border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
          <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Уведомления</div>
          <div style="display: flex; flex-direction: column; gap: 9px; margin-top: 12px;">
            <span style="display: flex; align-items: center; gap: 9px; font-size: 13px;"><span style="width: 16px; height: 16px; border-radius: 8px; border: 5px solid ${T.acc}; box-sizing: border-box;"></span>Только Control Center</span>
            <span style="font-size: 11.5px; color: ${T.mut};">Подключение внешнего канала не требуется</span>
          </div>
        </div>
      </div>

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 14px 16px; display: flex; align-items: center; gap: 10px;">
        <span style="color: ${T.mut}; display: flex;">${L.icon('chevR', 15)}</span>
        <span style="flex: 1 1 auto; font-size: 13px; color: ${T.sec};">Дополнительные настройки — таймаут, приоритет, политика сессии</span>
        <span style="font-size: 11.5px; color: ${T.mut};">свёрнуто</span>
      </div>
    </div>

    <aside style="flex: 0 0 356px; display: flex; flex-direction: column; gap: 14px;">
      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px;">
        <div style="font-size: 12.5px; font-weight: 600; letter-spacing: 0.02em; text-transform: uppercase; color: ${T.sec};">Что будет запущено</div>
        <div style="margin-top: 10px;">
          ${sumRow('Процесс', 'Обработка нового лида · v3')}
          ${sumRow('Координатор', 'Менеджер продаж')}
          ${sumRow('Участники', 'Аналитик продаж, Редактор предложений, Юридический консультант')}
          ${sumRow('Решение человека', 'Согласовать предложение')}
          ${sumRow('Файл', 'brief.pdf · проверен')}
          ${sumRow('Интеграции', 'Не требуются')}
        </div>
      </div>

      <div style="border: 1px solid ${T.line}; border-radius: 12px; background: ${T.bg}; padding: 16px; display: flex; flex-direction: column; gap: 9px;">
        <button style="width: 100%; height: 40px; border-radius: 9px; border: 0; background: ${T.acc}; color: #FFFFFF; font: inherit; font-size: 14px; font-weight: 600;">Запустить</button>
        <div style="display: flex; gap: 8px;">
          <button style="flex: 1 1 0; height: 34px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.ink}; font: inherit; font-size: 12.5px;">Сохранить черновик</button>
          <button style="flex: 1 1 0; height: 34px; border-radius: 9px; border: 1px solid ${T.line}; background: ${T.bg}; color: ${T.sec}; font: inherit; font-size: 12.5px;">Отменить</button>
        </div>
        <p style="margin: 4px 0 0; font-size: 11.5px; color: ${T.mut}; line-height: 1.5;">После запуска откроется рабочий экран запуска — там будет видно очередь и выполнение.</p>
      </div>
    </aside>
  </div>`,
}));
