# Caseflow API

A Go HTTP API backed by PostgreSQL. The server applies its numbered SQL migrations on startup.

## Run locally

Set `DATABASE_URL` in an ignored `.env` file, or in your environment:

```text
DATABASE_URL=postgres://caseflow:<password>@127.0.0.1:5432/caseflow?sslmode=disable
```

Then run `go run ./cmd/server`. The default port is `8080`; set `PORT` to change it. The database must already be running. The server creates the `users`, `sessions`, and `schema_migrations` tables as needed.

## Routes

| Method | Path | JSON body | Result |
| --- | --- | --- | --- |
| GET | `/health` | none | Database availability and pool statistics |
| POST | `/signup` | `{"email":"...","password":"..."}` | Creates a user (`201`) |
| POST | `/login` | `{"email":"...","password":"..."}` | Returns a bearer token and expiry |
| POST | `/logout` | none | Revokes the current bearer token (`204`) |
| PUT | `/me/password` | `{"current_password":"...","new_password":"..."}` | Changes the logged-in user's password and revokes all their sessions (`204`) |

Send the token from `/login` as `Authorization: Bearer <token>` on protected routes. Tokens expire after 24 hours. Passwords must be 12 to 72 bytes. JSON requests must use `Content-Type: application/json`.

Bearer tokens must be sent over HTTPS in production. Apply a rate limit to signup and login at the public edge, and keep `DATABASE_URL` out of source control.

## Tests

Run `go test ./...` for unit tests. To include the HTTP and PostgreSQL authentication flow, set `TEST_DATABASE_URL` to a **disposable database** before running the tests.