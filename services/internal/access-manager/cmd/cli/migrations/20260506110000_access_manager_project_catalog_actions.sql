-- +goose Up
INSERT INTO access_actions (
    id, key, display_name, description, resource_type, status, version, created_at, updated_at
) VALUES
    ('c450e805-344a-40ea-b843-81b8aa8fd49b', 'project.create', 'Создать проект', 'Создание проекта в организации.', 'project', 'active', 1, now(), now()),
    ('5d6e194e-2e5e-46e1-8cb3-1e2529862b1d', 'project.update', 'Обновить проект', 'Изменение карточки и состояния проекта.', 'project', 'active', 1, now(), now()),
    ('fdbf6452-ab80-44ab-9ff0-8f9f70c6ac7d', 'project.read', 'Читать проект', 'Чтение карточки проекта.', 'project', 'active', 1, now(), now()),
    ('9711b504-95b7-4cdc-bc90-3f6e1b9f7cb7', 'project.list', 'Список проектов', 'Чтение списка проектов.', 'project', 'active', 1, now(), now()),
    ('8009d18a-fb4f-4358-83ec-9af0e6264b81', 'repository.attach', 'Подключить репозиторий', 'Привязка provider-native репозитория к проекту.', 'repository', 'active', 1, now(), now()),
    ('5aff4168-7f8c-4799-ad3f-88a56be5c394', 'repository.update', 'Обновить репозиторий', 'Изменение безопасных полей привязки репозитория.', 'repository', 'active', 1, now(), now()),
    ('0edf7d66-4d6a-4034-864a-59a650d4acb4', 'repository.detach', 'Отключить репозиторий', 'Архивация привязки репозитория к проекту.', 'repository', 'active', 1, now(), now()),
    ('1804bd96-9c9e-451d-b89b-6ea3fb15b4b2', 'repository.read', 'Читать репозиторий', 'Чтение привязки репозитория.', 'repository', 'active', 1, now(), now()),
    ('61117d2a-2fe6-4fdc-a099-241e58e0dbb8', 'repository.list', 'Список репозиториев', 'Чтение списка репозиториев проекта.', 'repository', 'active', 1, now(), now()),
    ('6a0dccc9-bd0b-48c0-b716-0bf9bdd4c929', 'project.policy.import', 'Импортировать политику проекта', 'Сохранение проверенной проекции services.yaml.', 'services_policy', 'active', 1, now(), now()),
    ('6fb87ad4-2b77-4086-b874-b97ea66071b5', 'project.policy.read', 'Читать политику проекта', 'Чтение проверенной политики проекта и связанных представлений.', 'services_policy', 'active', 1, now(), now()),
    ('b5ca0bec-540e-4a98-beef-1f268d281082', 'project.policy.propose', 'Предложить правку политики', 'Создание предложения правки services.yaml.', 'services_policy', 'active', 1, now(), now()),
    ('da09b6a3-1988-44c2-a89c-439258b6879a', 'project.policy.override', 'Создать переопределение политики', 'Создание временного операторского переопределения политики.', 'policy_override', 'active', 1, now(), now()),
    ('1a20cdb1-8864-4909-ab2b-08d4cd35c0be', 'project.policy.override.read', 'Читать переопределения политики', 'Чтение временных операторских переопределений политики.', 'policy_override', 'active', 1, now(), now()),
    ('5240119e-4036-4b45-8cde-62a3b28196ba', 'project.policy.override.cancel', 'Отменить переопределение политики', 'Отмена временного операторского переопределения политики.', 'policy_override', 'active', 1, now(), now()),
    ('88f3905e-146d-4792-a677-ba2815758ca2', 'project.docs.update', 'Обновить источник документации', 'Создание или изменение источника проектной документации.', 'documentation_source', 'active', 1, now(), now()),
    ('1d508caa-2f84-43de-ae7e-cabd1abf35bb', 'project.docs.read', 'Читать источник документации', 'Чтение источников проектной документации.', 'documentation_source', 'active', 1, now(), now()),
    ('42631666-bca6-44a7-82ac-afe763e15dd2', 'project.workspace.read', 'Читать политику рабочего контура', 'Чтение состава источников для рабочего контура агента.', 'project', 'active', 1, now(), now()),
    ('12492a22-3401-425f-85fc-264387ba5507', 'project.branch_rules.update', 'Обновить правила веток', 'Создание или изменение правил веток.', 'branch_rules', 'active', 1, now(), now()),
    ('0611e333-b701-4963-b31f-4def7e86df4a', 'project.branch_rules.read', 'Читать правила веток', 'Чтение правил веток.', 'branch_rules', 'active', 1, now(), now()),
    ('97058dd1-5d86-46cc-8ab5-97b023b75873', 'project.release_policy.update', 'Обновить релизную политику', 'Создание или изменение релизной политики.', 'release_policy', 'active', 1, now(), now()),
    ('af2a176c-aa9a-44fd-8017-ae42a570f349', 'project.release_policy.read', 'Читать релизную политику', 'Чтение релизных политик.', 'release_policy', 'active', 1, now(), now()),
    ('04ec2de0-40e5-439c-87ce-90cb882cd3d7', 'project.release_line.update', 'Обновить релизную линию', 'Создание или изменение релизной линии.', 'release_line', 'active', 1, now(), now()),
    ('18d34797-af67-48b0-9f8d-7a74a3518ad6', 'project.release_line.read', 'Читать релизную линию', 'Чтение релизных линий.', 'release_line', 'active', 1, now(), now()),
    ('753046c6-1348-4e4b-9c56-2fc70a58b7aa', 'project.placement_policy.update', 'Обновить политику размещения', 'Создание или изменение политики размещения.', 'placement_policy', 'active', 1, now(), now()),
    ('302e16d4-6a13-403f-a3c2-c2003773a964', 'project.placement_policy.read', 'Читать политику размещения', 'Чтение политик размещения.', 'placement_policy', 'active', 1, now(), now())
ON CONFLICT (key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    resource_type = EXCLUDED.resource_type,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at
WHERE access_actions.id = EXCLUDED.id;

-- +goose Down
DELETE FROM access_actions
WHERE key IN (
    'project.create',
    'project.update',
    'project.read',
    'project.list',
    'repository.attach',
    'repository.update',
    'repository.detach',
    'repository.read',
    'repository.list',
    'project.policy.import',
    'project.policy.read',
    'project.policy.propose',
    'project.policy.override',
    'project.policy.override.read',
    'project.policy.override.cancel',
    'project.docs.update',
    'project.docs.read',
    'project.workspace.read',
    'project.branch_rules.update',
    'project.branch_rules.read',
    'project.release_policy.update',
    'project.release_policy.read',
    'project.release_line.update',
    'project.release_line.read',
    'project.placement_policy.update',
    'project.placement_policy.read'
);
