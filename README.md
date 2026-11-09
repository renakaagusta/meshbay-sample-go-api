# meshbay-sample-go-api

Minimal Go 1.23 API for the Meshbay vs Railway side-by-side test. Plain `net/http`, `pgx` and Postgres, no framework.

## Environment

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | yes | none | Postgres connection string, e.g. `postgres://user:password@host:5432/app` |
| `PORT` | no | `8080` | HTTP port to listen on |

## Run locally

```sh
go mod download
DATABASE_URL=postgres://postgres:postgres@localhost:5432/app go run .
```

The `items` table (`id serial primary key, name text not null, created_at timestamptz default now()`) is created
on start when it is missing.

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/health` | `200 {"status":"ok","version":"v1"}` |
| GET | `/api/items` | JSON array of items |
| POST | `/api/items` | Body `{"name":"..."}` → `201` with the created item |

```sh
curl localhost:8080/health
curl -X POST localhost:8080/api/items -H 'content-type: application/json' -d '{"name":"first"}'
curl localhost:8080/api/items
```

CORS is open (`Access-Control-Allow-Origin: *`). Change `applicationVersion` in `main.go` to test a redeploy.

## Docker

This repo is the only sample with a Dockerfile, on purpose (it tests the platform's Docker detection).

```sh
docker build -t meshbay-sample-go-api .
docker run --rm -e DATABASE_URL=postgres://user:password@host:5432/app -p 8080:8080 meshbay-sample-go-api
```
