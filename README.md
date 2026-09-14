<p align="center">
  <img src="assets/fturnpc_mockup.png" width="750" alt="FTurnPc Sing-box Interface" />
</p>

<h1 align="center">FTurnPc Sing-box</h1>

<p align="center">
  Десктопный VPN-клиент на базе <b>sing-box + FreeTurn</b>.<br>
  Системный трафик проходит через TUN-интерфейс sing-box, а транспорт до сервера передаётся через локальный FreeTurn relay и TURN-инфраструктуру.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-2.0.0-111111?style=for-the-badge" alt="Version 2.0.0">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/Wails-2.13-red?style=for-the-badge&logo=wails&logoColor=white" alt="Wails 2.13">
  <img src="https://img.shields.io/badge/sing--box-%3E%3D%201.14.0-222222?style=for-the-badge" alt="sing-box >= 1.14.0">
  <img src="https://img.shields.io/badge/Windows-primary-0078D4?style=for-the-badge&logo=windows&logoColor=white" alt="Windows">
</p>

---

## Что изменилось в Sing-box версии

Эта ветка больше не использует старую схему, где приложение само поднимало отдельный WireGuard-интерфейс `wg-turn`.

Теперь системный VPN-интерфейс создаёт **sing-box**:

```text
Приложения Windows
        │
        ▼
   fturn-tun
   (sing-box TUN)
        │
        ▼
Маршрутизация sing-box
        │
        ├──────────────► direct
        │                 ├─ RU bypass
        │                 ├─ bypass приложений
        │                 └─ private/local traffic
        │
        ▼
proxy outbound / endpoint
(VLESS / WG / Trojan / SS / HY2 / TUIC / ...)
        │
        ▼
127.0.0.1:9000
        │
        ▼
FreeTurn client
        │
        ▼
TURN / DTLS / RTP transport
        │
        ▼
FreeTurn server / VPS
        │
        ▼
Internet
```

`127.0.0.1:9000` — это **намеренная часть архитектуры**. sing-box не подключается к реальному proxy-host напрямую: сетевой destination перенаправляется на локальный FreeTurn relay, а исходный host сохраняется там, где он нужен для TLS SNI/Reality.

---

## Архитектура подключения

При подключении FTurnPc выполняет последовательность:

1. Загружает профиль и определяет тип `sb`/WireGuard-конфига.
2. Определяет FreeTurn relay mode: `tcp` или `udp`.
3. Генерирует итоговый sing-box config с TUN, DNS, route rules и bypass-правилами.
4. Проверяет конфиг через `sing-box check`.
5. Запускает FreeTurn на `127.0.0.1:9000`.
6. Ждёт реальной готовности FreeTurn transport.
7. Запускает sing-box и ждёт создания `fturn-tun`.
8. После готовности переводит UI в состояние подключённого туннеля.

Если sing-box неожиданно завершается, backend считает транспорт неработоспособным и запускает механизм восстановления соединения.

---

## Поддерживаемые proxy-конфигурации

### URI

Парсер приложения поддерживает:

- `vless://`
- `trojan://`
- `ss://` (Shadowsocks, включая SIP002 Base64 userinfo)
- `hysteria2://`
- `hy2://`
- `tuic://`

### VLESS

Для VLESS поддерживаются основные параметры:

- TCP
- TLS
- Reality (`pbk`, `sid`)
- SNI
- uTLS fingerprint (`fp`)
- WebSocket (`type=ws`, `host`, `path`)
- gRPC (`serviceName`)
- HTTP/H2 transport
- `flow`

Оригинальный hostname сохраняется как TLS `server_name`, даже несмотря на то, что реальный dial destination меняется на `127.0.0.1:9000`.

Новые Xray-specific transports вроде `xhttp`/`splithttp` могут требовать raw sing-box JSON и не считаются частью URI-парсера FTurnPc.

### WireGuard

Поддерживаются:

- legacy WireGuard profile в поле `wg`;
- sing-box WireGuard endpoint schema;
- автоматическое преобразование старого WireGuard outbound в endpoint schema sing-box 1.14+.

Для FreeTurn WireGuard peer перенаправляется на:

```text
127.0.0.1:9000/UDP
```

### Raw sing-box JSON

Профиль может содержать готовый sing-box JSON в поле `sb`. FTurnPc добавляет к пользовательскому proxy-конфигу системные TUN/DNS/route rules и перенаправляет proxy endpoint на локальный FreeTurn relay.

Для raw JSON relay mode определяется по типу proxy outbound/endpoint. Среди распознаваемых типов есть VLESS, VMess, Trojan, Shadowsocks, WireGuard, Hysteria/Hysteria2, TUIC, AnyTLS, ShadowTLS, HTTP и SOCKS.

---

## FreeTurn `mode` и `transport` — разные параметры

Это важно для данной версии.

`mode` определяет протокол локального relay на `127.0.0.1:9000`:

| Proxy protocol | FreeTurn mode |
| --- | --- |
| VLESS / VMess / Trojan / Shadowsocks / AnyTLS / ShadowTLS / HTTP / SOCKS | `tcp` |
| WireGuard / Hysteria / Hysteria2 / HY2 / TUIC | `udp` |

`transport` — отдельная настройка транспорта FreeTurn/TURN и передаётся через `-transport`.

Например это валидная комбинация:

```text
relay mode=tcp
TURN transport=udp
```

Для VLESS это означает, что sing-box подключается к локальному FreeTurn listener по TCP, а сам FreeTurn может использовать UDP для своей TURN-транспортной части.

---

## TUN и защита loopback

Sing-box создаёт TUN-интерфейс:

```text
fturn-tun
```

Поскольку proxy endpoint FTurnPc намеренно находится на localhost, конфиг дополнительно защищён от routing loop:

```json
{
  "route_exclude_address": ["127.0.0.0/8"]
}
```

Proxy outbounds/endpoints, направленные на FreeTurn, также получают explicit loopback bind:

```json
{
  "inet4_bind_address": "127.0.0.1"
}
```

Это предотвращает ситуацию, когда `auto_detect_interface` sing-box пытается отправить пакет к `127.0.0.1` через физический сетевой интерфейс Windows.

Сам процесс FreeTurn также добавляется в раннее `direct`-правило по именам поддерживаемых бинарников, чтобы его TURN-трафик не зацикливался обратно в TUN.

---

## Bypass RU

В настройках можно включить обход российских ресурсов.

Текущая версия использует sing-box routing, а не старые тысячи `netsh route` записей.

Основной GeoIP rule-set хранится в исходниках:

```text
assets/freeturn/geoip-ru.srs
```

При сборке он встраивается в приложение через `go:embed`, а при запуске материализуется в config directory:

```text
%AppData%\FTurnPc-singbox\geoip-ru.srs
```

При включённом Bypass RU используются:

- `geoip-ru.srs` для российских IP-сетей;
- domain suffix fallback для `ru`, `su`, `xn--p1ai`.

В логах можно увидеть активный режим:

```text
[SB] RU bypass: geoip + domains (...\geoip-ru.srs)
```

Если SRS не загрузился, backend сообщает, что используется только domain fallback.

---

## Bypass приложений

В боковом меню есть отдельное окно **«Обход»**.

Можно добавить:

```text
steam.exe
Discord.exe
chrome.exe
```

или вставить полный путь:

```text
C:\Program Files\Google\Chrome\Application\chrome.exe
```

Backend сохраняет basename процесса и создаёт case-insensitive `process_path_regex` rule с outbound `direct`.

Настройка глобальная для всех профилей и хранится в:

```text
%AppData%\FTurnPc-singbox\bypass-apps.json
```

Изменения применяются при следующем запуске/переподключении туннеля.

> [!NOTE]
> Это split tunneling на уровне sing-box route engine. Пакет приложения может попасть в TUN для классификации, после чего будет отправлен через `direct`. Это не WFP/WinDivert-level исключение процесса из Windows networking stack.

---

## DNS и маршрутизация

FTurnPc собирает собственный sing-box config вокруг выбранного proxy:

- TUN с `auto_route`;
- strict routing;
- DNS hijack для трафика внутри туннеля;
- ранние direct rules для FreeTurn и bypass приложений;
- direct для private networks;
- RU bypass, если включён;
- основной `proxy` как final route.

Loopback `127.0.0.0/8` исключается из TUN auto-route отдельно, до routing engine.

---

## Мониторинг трафика

Главный экран показывает:

```text
скорость: KB/s или MB/s
объём:    KB / MB / GB
```

На Windows значения берутся из byte counters интерфейса `fturn-tun`.

Текущая версия использует baseline при старте stats-loop, поэтому общий объём считается **от текущего подключения**, а не от lifetime Windows TUN adapter.

32-bit overflow Windows interface counters и reset/recreate TUN также обрабатываются отдельно.

---

## Логи и автоматическое переподключение

Один пользовательский `Connect` создаёт одну log-сессию.

Все автоматические reconnect attempts продолжают писать в тот же файл до ручного `Disconnect`.

Логи находятся в:

```text
%AppData%\FTurnPc-singbox\logs\
```

Обычные TCP teardown сообщения sing-box вроде:

```text
connection download closed ... forcibly closed by the remote host
connection upload closed ... aborted by the software in your host machine
connection reset by peer
broken pipe
```

не подсвечиваются в UI как критические ошибки. Они остаются доступными для диагностики в session log.

При этом реальные ошибки продолжают выделяться:

```text
dial ... i/o timeout
TLS / Reality handshake failure
WireGuard handshake failure
TUN setup failure
sing-box startup failure
fatal errors
```

---

## Требования

Основная и наиболее тестируемая платформа — **Windows**.

Для работы нужны:

- Windows с правами, достаточными для создания TUN-интерфейса;
- FreeTurn client;
- sing-box `>= 1.14.0`;
- серверная часть FreeTurn с mode, совпадающим с клиентской конфигурацией.

FTurnPc ищет sing-box в следующем порядке:

```text
<exe-dir>\sing-box.exe
<exe-dir>\assets\singbox\sing-box.exe
%AppData%\FTurnPc-singbox\core\sing-box.exe
PATH
```

На Windows FreeTurn может называться:

```text
freeturnclient.exe
client-windows-amd64.exe
client.exe
```

Поиск выполняется рядом с приложением, в `assets/freeturn` и через `PATH`.

---

## Быстрый старт на Windows

1. Положите `sing-box.exe` версии 1.14+ рядом с приложением либо в один из поддерживаемых путей.
2. Положите FreeTurn client рядом с приложением или в `assets/freeturn`.
3. Запустите FTurnPc с правами, необходимыми для TUN.
4. Добавьте профиль кнопкой `+` или импортируйте `freeturn://` ссылку.
5. При необходимости включите RU bypass и настройте список bypass приложений.
6. Нажмите кнопку подключения.
7. При проблемах откройте вкладку логов и session log в `%AppData%\FTurnPc-singbox\logs`.

При нормальном запуске в логах будут этапы примерно такого вида:

```text
===== SESSION START =====
[FT] relay mode=...
[SB] sing-box check OK
[FT] Транспорт FreeTurn готов; запускаем sing-box...
[SB] Запуск sing-box TUN...
✓ Туннель активен
```

---

## Формат профиля

Профили сохраняются в:

```text
%AppData%\FTurnPc-singbox\profiles\
```

Пример профиля с VLESS:

```json
{
  "name": "Example VLESS",
  "provider": "vk",
  "peer": "TURN_PEER_IP:PORT",
  "transport": "udp",
  "links": "https://vk.ru/call/join/...",
  "obf": "rtpopus",
  "key": "...",
  "cid": "...",
  "sb": "vless://UUID@example.com:443?security=tls&type=ws&host=cdn.example&path=%2Fws"
}
```

Пример legacy WireGuard-профиля:

```json
{
  "name": "Example WG",
  "provider": "vk",
  "peer": "TURN_PEER_IP:PORT",
  "transport": "udp",
  "links": "https://vk.ru/call/join/...",
  "obf": "rtpopus",
  "key": "...",
  "cid": "...",
  "wg": "[Interface]\nPrivateKey = ...\nAddress = 10.0.0.2/32\n\n[Peer]\nPublicKey = ...\nAllowedIPs = 0.0.0.0/0"
}
```

Поле `mode` можно передать явно (`tcp` или `udp`), но обычно backend определяет его автоматически по типу proxy. Если явно заданный mode конфликтует с proxy protocol, подключение отклоняется вместо запуска некорректной схемы.

---

## Разработка

### Зависимости

```text
Go 1.26+
Node.js / npm
Wails v2.13+
sing-box 1.14+
```

Frontend текущей версии использует React 19, TypeScript и Vite.

### Запуск dev-режима

```bash
cd frontend
npm install
cd ..
wails dev
```

### Backend tests

```bash
go test ./backend
```

Для Windows/race-проверок, если окружение поддерживает race detector:

```bash
go test -race ./backend
```

### Frontend build

```bash
cd frontend
npm run build
```

### Windows build

```bash
wails build -platform windows/amd64 -o FTurnPc-singbox.exe
```

`wails.json` текущей ветки использует product name `FTurnPc-singbox` и версию `2.0.0`.

---

## Проверка sing-box config

Перед каждым подключением FTurnPc автоматически выполняет:

```bash
sing-box check -c <generated-config>
```

Если конфиг несовместим с установленной версией sing-box, FreeTurn/TUN не должны запускаться как успешная VPN-сессия.

Минимальная поддерживаемая версия sing-box в backend:

```text
1.14.0
```

---

## Известные ограничения

> [!WARNING]
> `strict_route` и teardown при падении sing-box улучшают fail-closed поведение, но это **не полноценный независимый OS-level kill switch**.
>
> После полного завершения sing-box его TUN/routes исчезают. Для настоящего kill switch требуется отдельный Windows WFP/firewall слой, который живёт независимо от процесса sing-box.

Также стоит учитывать:

- application bypass сейчас реализован через sing-box routing, а не WFP;
- URI-парсер поддерживает не все новые Xray-specific transport extensions;
- Linux/macOS имеют отдельные entrypoints и собираемые backend-части, но основная runtime-разработка и тестирование этой версии ориентированы на Windows.

---

## Диагностика

Если туннель не работает, сначала проверьте session log и найдите последний успешно пройденный этап.

### FreeTurn не становится готовым

Ищите сообщения о TURN allocation/session readiness. Для UDP relay готовность определяется по активному TURN allocation, для TCP — по рабочей TCP session/pool.

### sing-box не стартует

Проверьте:

```text
версию sing-box
sing-box check output
права на создание TUN
валидность proxy URI / raw JSON
```

### `WSAEADDRNOTAVAIL` при подключении к `127.0.0.1:9000`

Текущая версия добавляет loopback hardening (`route_exclude_address=127.0.0.0/8` и `inet4_bind_address=127.0.0.1`). Если такая ошибка появилась снова, приложите generated sing-box config и session log.

### `using outbound/direct ... i/o timeout`

Это означает, что route rule выбрал `direct`, но физическое direct-соединение не установилось вовремя. Это отличается от ситуации, когда bypass rule вообще не сработал.

---

## Структура проекта

```text
backend/
  singbox_config.go       генерация sing-box config
  singbox_tun.go          lifecycle процесса sing-box
  singbox_loopback.go     localhost/TUN hardening
  freeturn_mode.go        выбор tcp/udp relay mode
  engine_freeturn.go      lifecycle FreeTurn
  engine_parser.go        readiness/log parser FreeTurn
  engine_stats.go         TUN traffic counters
  bypass_apps.go          пользовательский split tunneling
  ru_bypass.go            RU rule-set helpers
  geoip_asset.go          materialization встроенного SRS
  orchestrator.go         session/reconnect lifecycle

frontend/src/
  pages/Connect.tsx       главный экран
  pages/Logs.tsx          журнал
  modals/BypassApps.tsx   окно обхода приложений

assets/freeturn/
  geoip-ru.srs            RU GeoIP rule-set
```

---

## Безопасность профилей и логов

Конфигурации могут содержать UUID, ключи WireGuard, passwords, Reality public parameters, FreeTurn links и другие чувствительные данные.

Не публикуйте без необходимости:

- содержимое `%AppData%\FTurnPc-singbox\profiles`;
- generated sing-box config;
- полные session logs;
- FreeTurn launch arguments с секретами;
- приватные WireGuard keys.

В debug-выводе FTurnPc старается маскировать известные чувствительные параметры, но перед публикацией логов всё равно рекомендуется их просмотреть.

---

## Статус ветки

README описывает текущую архитектуру ветки:

```text
fix/singbox-freeturn-readiness
```

По сравнению со старой `singbox` версия включает исправленный FreeTurn readiness, sing-box loopback bypass, persistent session logging, application bypass UI, embedded RU SRS и session-based traffic counters.

---

## Благодарности

- [samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy) — FreeTurn client/server transport.
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box) — TUN, routing, DNS и proxy protocols.
- [PWDTT](https://github.com/luminescq/PWDTT) — исходные идеи/части UI.

## Лицензия

Проект распространяется под лицензией **GNU General Public License v3.0**.

---

> [!IMPORTANT]
> Используйте FTurnPc только для сетей, серверов и инфраструктуры, к которым у вас есть законное право доступа и использования.
