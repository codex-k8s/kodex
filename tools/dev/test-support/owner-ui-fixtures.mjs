import { Readable } from "node:stream";

// Каждому тесту выдаются новые искусственные pins, без чтения профиля или секретов.
export function projectSecretInputProfile(
  origin = "https://kodex.127.0.0.2.nip.io",
) {
  return {
    cdpEndpoint: "http://127.0.0.1:9222",
    chromePID: 1234,
    chromeStartTicks: "4321",
    pageURL: `${origin}/projects/prj_synthetic/secrets`,
    organizationRef: "org_synthetic",
    ownerRef: "usr_synthetic",
    projectRef: "prj_synthetic",
    environmentRef: "renv_synthetic",
    environmentVersion: 2,
    versionRef: "renvv_synthetic",
    environmentDigest: "a".repeat(64),
    agentRef: "agt_synthetic",
    agentVersion: 5,
    bindingRef: "aenv_synthetic",
    bindingVersion: 3,
    bindingDigest: "b".repeat(64),
  };
}

// Это положительный вход тестов допуска, не доказательство настоящих полномочий.
// Вложенные объекты/массивы создаются заново: негативные мутации не текут между тестами.
export function projectSecretOwnerProof(profile) {
  return {
    ownerRef: profile.ownerRef,
    organizationRef: profile.organizationRef,
    platformRole: "OWNER",
    environment: {
      ref: profile.environmentRef,
      organizationRef: profile.organizationRef,
      projectRef: profile.projectRef,
      scopeKind: "PROJECT",
      name: "selfdev-write",
      state: "ACTIVE",
      ready: true,
      version: profile.environmentVersion,
      revisionRef: profile.versionRef,
      digest: profile.environmentDigest,
    },
    agentVersion: profile.agentVersion,
    binding: {
      ref: profile.bindingRef,
      version: profile.bindingVersion,
      agentRef: profile.agentRef,
      environmentRef: profile.environmentRef,
      versionRef: profile.versionRef,
      digest: profile.bindingDigest,
    },
    assignedAgents: {
      items: [
        {
          ref: profile.agentRef,
          version: profile.agentVersion,
          projectRef: profile.projectRef,
          system: false,
        },
      ],
      nextPageToken: "",
    },
    impact: {
      environmentRef: profile.environmentRef,
      environmentVersion: profile.environmentVersion,
      targetVersionRef: profile.versionRef,
      targetDigest: profile.environmentDigest,
      total: 0,
      consumers: [],
      nextPageToken: "",
    },
    secretAbsent: true,
  };
}

// Только поддельный запрос: настройки маршрута и намеренно неверные поля задаёт тест.
// changes заменяет поле целиком, как прежний Object.assign, а не исправляет malformed input.
export function ownerInputRequest({
  host,
  origin,
  path,
  port,
  body,
  changes = {},
}) {
  const request = Readable.from([body]);
  Object.assign(
    request,
    {
      socket: {
        servername: host,
        getProtocol: () => "TLSv1.3",
        localAddress: "127.0.0.2",
        localPort: port,
        remoteAddress: "127.0.0.1",
        remotePort: 45678,
        destroyed: false,
      },
      method: "POST",
      url: path,
      headers: {
        host: `${host}:${port}`,
        origin,
        "content-type": "application/json",
        "content-length": String(body.length),
      },
    },
    changes,
  );
  request.rawHeaders = Object.entries(request.headers).flat();
  return request;
}
