# poker-with-friends

Покер (Texas Hold'em No-Limit) с друзьями в браузере, только на фишки. Бэкенд на Go, фронтенд на React + TypeScript.

## Устройство

- `engine/` — чистая логика покера без сети: карты, тасовка, оценка рук, торговля (`Game`), банки и выплаты. Не знает о HTTP и WebSocket.
- `internal/server/` — HTTP API (комнаты, вход), WebSocket `/ws`, стол-актор (`table.go`: всё состояние стола принадлежит одной горутине, изменения только через `do`), вид стола для игрока (`view.go`: чужие карты не отправляются до вскрытия), отдача собранного фронтенда (`static.go`).
- `internal/store/` — PostgreSQL на pgx: схема в `migrations/` (goose, встроены через `embed`), запросы. Сервер видит его через интерфейс `Store` (`internal/server/persist.go`); записи идут через очередь `recorder`, чтобы стол не ждал базу.
- `cmd/server/` — точка входа, флаги `-addr` и `-static`, переменная `DATABASE_URL` (без неё всё в памяти).
- `frontend/` — клиент на Vite. Чистая логика в `src/lib/` (с тестами), компоненты в `src/components/`.
- `docs/API.md` — протокол между клиентом и сервером, `docs/FRONTEND*.md` — ТЗ фронтенда.

Сервер — единственный источник правды: клиент присылает намерение, сервер проверяет правила и рассылает каждому полное состояние стола.

## Команды

```bash
go test -short ./...                 # быстро
go test -race -cover ./...           # как в CI, включая полный перебор рук
gofmt -l . && go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...

cd frontend && npm ci --ignore-scripts && npm test && npm run build

go run ./cmd/server -static frontend/dist   # игра целиком на :8080
docker compose up --build                  # то же в Docker, вместе с PostgreSQL

# прод на VPS: Caddy с HTTPS, нужен .env с SITE_ADDRESS и POSTGRES_PASSWORD
docker compose -f compose.yaml -f compose.prod.yaml up -d --build
scripts/deploy.sh                          # на сервере: git pull + пересборка

# тесты базы: без TEST_DATABASE_URL они пропускаются
docker run -d --name poker-pg-test -e POSTGRES_PASSWORD=test -e POSTGRES_DB=poker_test -p 55432:5432 postgres:18.6-alpine
TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/poker_test?sslmode=disable' go test ./internal/store/
```

Нужен Go 1.27.2+ и Node 24.

## Правила

- Каждое изменение — в своей ветке (`feat/…`, `fix/…`, `docs/…`, `test/…`, `ci/…`) от свежего `main`, потом слияние в `main`. Напрямую в `main` не коммитить.
- Сообщения коммитов в стиле Conventional Commits: `feat: …`, `fix: …`, `docs: …`.
- Сначала тест, потом код. Для логики, которую легко сломать (банки, торговля, расстановка мест), полезно проверить тест на намеренно испорченной копии кода: он должен падать.
- CI (`.github/workflows/ci.yml`) должен быть зелёным: gofmt, go vet, staticcheck, тесты Go с `-race`, тесты и сборка фронтенда.
- `node_modules` и `dist` не коммитятся, переносы строк только LF (`.gitattributes`).

## Подводные камни

- Сервер отклоняет WebSocket с чужого Origin (403). В разработке фронтенд ходит через прокси Vite **без** `changeOrigin` (`frontend/vite.config.ts`).
- Коды закрытия WebSocket: 4001 — вошли с другого окна, 4004 — комнаты нет, 1008 — неверный токен. После них клиент не переподключается сам.
- Без `DATABASE_URL` комнаты живут только в памяти. С базой после перезапуска комнаты восстанавливаются, а раздача, шедшая в момент перезапуска, теряется; новая начинается, когда подключится игрок.
- Схему базы меняем только новой миграцией в `internal/store/migrations/`, старые файлы не трогаем. В базе хранится только SHA-256 токена.
- Таймер хода 30 с задан на сервере (`Options.TurnTimeout`), фронтенд рассчитывает на это значение (`TURN_MS`).
- Прод запускается с двумя файлами compose (`compose.yaml` + `compose.prod.yaml`). Наружу открыт только Caddy (80/443), порт 8080 не публикуется. Секреты лежат только в `.env` на сервере, в git их нет. Другие сайты на том же сервере подключаются файлами `/opt/caddy-sites/*.caddy` и сетью Docker `web`.
- Анимации во фронтенде — только `transform`/`opacity`, 150–400 мс. Они никогда не задерживают показ состояния и отключаются после (пере)подключения и при `prefers-reduced-motion`.
