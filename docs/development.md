# Сборка и сопровождение Windows-версии

[Все разделы документации](README.md)

## Локальная разработка

Проект ориентирован на Windows x64. Требуются Go 1.26+, Node.js/npm, Wails CLI 2.13 и его Windows-зависимости. Для NSIS installer нужен NSIS. Версии CI смотрите в [workflow](../.github/workflows/fix-nsis.yml).

Из корня репозитория в PowerShell:

```powershell
Set-Location frontend
npm ci
Set-Location ..
go test ./backend
wails dev
```

Для подключения нужны профиль, сервер и внешние Windows-ядра. Не коммитьте `client_config.json`, секретные профили и временные конфигурации. Тесты не заменяют проверку подключения.

## Проверки перед выпуском

```powershell
go test ./backend
npm --prefix frontend run build
```

Проверка схемы конфигураций должна использовать **тот же sing-box, что входит в пакет**:

```powershell
$env:SING_BOX_BIN = 'C:\Tools\sing-box-1.14.0\sing-box.exe'
go test -tags=integration ./backend -run TestSingBoxSchema -v
```

Путь — пример. Тест ориентирован на `1.14.x`. При осознанном переходе задайте также `SING_BOX_EXPECTED_VERSION` и проверьте всю матрицу. Это выбор тестируемого бинарника, не обход ошибок схемы.

Изменение backend API требует обновления Wails bindings в `frontend/wailsjs`; штатная сборка Wails генерирует их. Проверьте diff и frontend build. При изменении статистики, готовности или маршрутов добавьте backend-тесты.

## Windows build

```powershell
wails build -platform windows/amd64 -nsis
```

На чистом checkout нужно подготовить внешние файлы. NSIS использует `build/bin/client-windows-amd64.exe`, RU CIDR/атрибуцию в `build/bin` и sing-box из `sing-box-1.14.0-windows-amd64`. Следуйте workflow и [NSIS-сценарию](../build/windows/installer/project.nsi).

Для запуска без installer разместите ядра по [правилам поиска](cores.md). Не полагайтесь на случайный `PATH`-бинарник при проверке релиза.

## Смена ядер в пакете

В [workflow](../.github/workflows/fix-nsis.yml) закреплены:

- `FREETURN_CORE_COMMIT` — ревизия `LiTarPc/fturn-core`;
- `SINGBOX_VERSION` — версия sing-box;
- маркировка FreeTurn в `-X main.version=...` и ожидаемый `-version`;
- `PREVIEW_TAG` — тег rolling preview.

Для FreeTurn обновите ревизию, её маркировку и проверку версии. Простое изменение номера не добавляет функции. При смене репозитория проверьте checkout, `CoreRepo` в [обновляторе](../backend/core_update.go), допустимые URL/asset names, CLI, готовность по логам и серверную совместимость.

Для sing-box обновите `SINGBOX_VERSION`, пути упаковки NSIS и прогоните интеграционные тесты на выбранном бинарнике. При изменении схемы адаптируйте генератор, не только номер. Учтите [минимальную версию](../backend/singbox_util.go).

## Публикация ветки

Push в `fix/singbox-freeturn-readiness` запускает Windows workflow и обновляет [rolling preview](https://github.com/LiTarPc/FTurnPc/releases/tag/singbox-fix-latest). Поэтому push способен изменить опубликованный installer, а не только сохранить коммиты.

Перед публикацией проверьте diff: без секретов, локальных конфигов и несвязанных изменений. Сохраните предыдущий рабочий пакет и ядра.

Ручной smoke test: импорт, подключение, передача данных, рост счётчиков, DNS, нужный обход, переподключение, отключение и завершение процессов. TCP и UDP проверяйте отдельно с соответствующей серверной конфигурацией.
