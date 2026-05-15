# sam-monitor

A small Go service that polls SAM.gov for matching opportunities, alerts via SMTP, and
presents a server-rendered HTMX UI for triage and prime-POC tracking. Also tracks the
registration status (SAM/CAGE) of one or more UEIs and alerts on status changes.

Designed for Yield LLC (UEI `TA9TQJR2GL18`) and deployed at `sam.yield-llc.com`.

## What it does

- Polls SAM.gov `/opportunities/v2/search` every 4h for each row in `saved_search`,
  inserts new notices into `opportunity`, and emails on insert.
- Polls SAM.gov `/entity-information/v4/entities` every 6h for each row in
  `tracked_entity`, records changes in `status_event`, and emails on status or CAGE flip.
- Server-rendered UI (`html/template` + HTMX + Pico CSS) for browsing opportunities,
  marking status (new / interested / pursuing / submitted / ignore), and viewing prime POCs.

## Layout

```
.
├── main.go                          # entry point: db + poller + tracker + http
├── Dockerfile                       # distroless multi-stage
├── internal/
│   ├── db/                          # pgxpool + embedded migrations
│   │   ├── db.go
│   │   └── migrations/*.sql
│   ├── sam/                         # SAM.gov API client
│   │   └── client.go
│   ├── poller/                      # 4h opportunity poller
│   │   └── poll.go
│   ├── regstatus/                   # 6h entity registration tracker
│   │   └── regstatus.go
│   ├── alert/                       # SMTP alerter
│   │   └── smtp.go
│   └── web/                         # html/template HTMX handlers
│       ├── handlers.go
│       └── templates/*.html
└── .github/workflows/build.yml      # CI: test + push image to ghcr
```

## Local development

Requires Go 1.23+ and a Postgres 16/17 (uuid-ossp extension).

```bash
docker run -d --name pg-test -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:17
export DATABASE_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable"
export SAM_API_KEY="..."           # api.data.gov key
export SMTP_HOST=smtp.gmail.com
export SMTP_PORT=587
export SMTP_USER=lucas@yield-llc.com
export SMTP_PASS="..."             # Google app password
export SMTP_FROM=lucas@yield-llc.com
export SMTP_TO=lucas@yield-llc.com
go run .
```

Then browse <http://localhost:8080/>.

`/health` is unauthenticated and returns `{"status":"ok"}`.

## Environment variables

| Var | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | pgx-style DSN |
| `SAM_API_KEY` | yes | api.data.gov key for SAM.gov |
| `SMTP_HOST` | optional | omit to disable alerting |
| `SMTP_PORT` | optional | usually `587` |
| `SMTP_USER`, `SMTP_PASS` | optional | for `smtp.PlainAuth` |
| `SMTP_FROM`, `SMTP_TO` | optional | `SMTP_TO` may be comma-separated |
| `HTTP_ADDR` | optional | defaults to `:8080` |

## Seed data

Migration `0003_seed_searches.sql` seeds the 4 saved searches from
`research/03-opportunity-research.md` §2. Migration `0004_seed_primes.sql` seeds the 13
primes from §1. Migration `0002_regstatus.sql` seeds Yield LLC's UEI for status tracking.

## Build

```bash
go build ./...
go vet ./...
go test ./...
```

Integration test (hits real SAM.gov):

```bash
SAM_API_KEY=xxx go test -tags=integration -v ./internal/sam/
```

## Deploy

Image is built and pushed by `.github/workflows/build.yml` on every push to `main` as
`ghcr.io/yieldllc/sam-monitor:<sha>` and `:latest`. Gitops manifests, CNPG cluster, and
Cloudflare tunnel ingress live in the `yieldllc/gitops` repo (`clusters/homelab/sam-monitor/`).
