<p align="center">
  <img src="assets/fturnpc_mockup.png" width="720" alt="FTurnPc" />
</p>

<h1 align="center">FTurnPc Sing-box</h1>

<p align="center">
  Windows VPN-клиент на базе <b>sing-box + FreeTurn</b>.<br>
  sing-box создаёт системный TUN, а FreeTurn переносит proxy-трафик через TURN-инфраструктуру.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-2.0.0-111111?style=flat-square" alt="2.0.0">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Wails-2.13-red?style=flat-square" alt="Wails">
  <img src="https://img.shields.io/badge/sing--box-%3E%3D1.14.0-222222?style=flat-square" alt="sing-box">
  <img src="https://img.shields.io/badge/Windows-x64-0078D4?style=flat-square&logo=windows&logoColor=white" alt="Windows">
</p>

## Как это работает

```text
Windows apps
    ↓
fturn-tun (sing-box TUN)
    ↓
sing-box routing
    ├─ direct: RU bypass / приложения / private networks
    ↓
proxy: VLESS / WG / Trojan / SS / HY2 / TUIC / ...
    ↓
127.0.0.1:9000
    ↓
FreeTurn client
    ↓
TURN transport
    ↓
FreeTurn server / VPS
    ↓
Internet
```

`127.0.0.1:9000` — намеренная часть архитектуры. Proxy endpoint перенаправляется в локальный FreeTurn relay, при этом исходный hostname сохраняется для TLS/SNI/Reality там, где это требуется.

FreeTurn запускается раньше sing-box. После готовности transport приложение поднимает `fturn-tun`. Для TURN-серверов FreeTurn создаёт отдельные `/32` host routes через физический gateway, чтобы его собственный транспорт не попадал обратно в TUN.

## Поддержка протоколов

URI:

- `vless://`
- `trojan://`
- `ss://`
- `hysteria2://` / `hy2://`
- `tuic://`

Также поддерживаются legacy WireGuard-профили и raw sing-box JSON.

FreeTurn relay mode определяется автоматически:

| Proxy | FreeTurn mode |
| --- | --- |
| VLESS, VMess, Trojan, Shadowsocks, AnyTLS, ShadowTLS, HTTP, SOCKS | `tcp` |
| WireGuard, Hysteria/Hysteria2, HY2, TUIC | `udp` |

`mode` и `transport` — разные параметры: первый задаёт локальный relay, второй — транспорт FreeTurn/TURN.

## Маршрутизация

- TUN: `fturn-tun`
- `auto_route` + strict routing
- DNS hijack внутри туннеля
- `127.0.0.0/8` исключён из TUN для защиты localhost relay
- FreeTurn process направляется в `direct`
- TURN IP получают отдельные `/32` маршруты через физический gateway
- private networks идут напрямую
- RU bypass — через встроенный `geoip-ru.srs` + домены `.ru`, `.su`, `.рф`
- bypass приложений — через sing-box `process_path_regex -> direct`

Bypass приложений хранится в:

```text
%AppData%\FTurnPc-singbox\bypass-apps.json
```

Это split tunneling на уровне sing-box routing, а не WFP/WinDivert исключение процесса из Windows networking stack.

## Трафик и логи

Статистика берётся напрямую с `fturn-tun`. На Windows используются 64-битные interface counters, а UI показывает скорость и объём с момента текущего подключения.

Логи одной пользовательской сессии сохраняются через все auto-reconnect попытки:

```text
%AppData%\FTurnPc-singbox\logs\
```

Обычные TCP teardown/reset сообщения не считаются фатальным падением туннеля; реальные ошибки TUN, TLS, WireGuard, startup и timeout остаются видимыми.

## Требования

- Windows x64
- права для создания TUN и host routes
- Go 1.26+ для сборки
- Wails 2.13+
- sing-box 1.14+
- FreeTurn client/server

Приложение ищет FreeTurn как `freeturnclient.exe`, `client-windows-amd64.exe` или `client.exe`.

## Установка

Готовая тестовая Windows-сборка публикуется как rolling prerelease:

**[Sing-box Fix Preview](https://github.com/LiTarPc/FTurnPc/releases/tag/singbox-fix-latest)**

При каждом push в тестовую ветку автоматически собирается новый NSIS installer и SHA256.

Для ручного запуска:

1. положите `sing-box.exe` 1.14+ и FreeTurn client рядом с приложением;
2. запустите FTurnPc;
3. импортируйте `freeturn://` профиль или добавьте профиль вручную;
4. подключитесь и при необходимости включите RU/application bypass.

## Разработка

```bash
cd frontend
npm install
cd ..

go test ./backend
wails dev
```

Windows build:

```bash
wails build -platform windows/amd64 -nsis
```

Перед подключением backend автоматически выполняет:

```bash
sing-box check -c <generated-config>
```

## Структура

```text
backend/
  engine_freeturn.go   FreeTurn lifecycle
  engine_parser.go     readiness/log parser
  engine_stats.go      fturn-tun traffic counters
  singbox_config.go    sing-box config generation
  singbox_tun.go       sing-box lifecycle
  bypass_apps.go       application bypass
  ru_bypass.go         RU bypass

frontend/src/
  pages/Connect.tsx
  pages/Logs.tsx
  modals/BypassApps.tsx
```

## Ограничения

- полноценного независимого OS-level kill switch пока нет;
- application bypass работает на уровне sing-box routing;
- не все Xray-specific transports поддерживаются URI-парсером;
- основная runtime-разработка и тестирование ориентированы на Windows.

## Безопасность

Профили и generated configs могут содержать UUID, WireGuard keys, passwords, FreeTurn links и другие чувствительные данные. Перед публикацией логов или конфигов удаляйте секреты.

## Credits

- [samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy)
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- [PWDTT](https://github.com/luminescq/PWDTT)

## License

GNU GPLv3.
