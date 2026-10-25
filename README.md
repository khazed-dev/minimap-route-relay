# MiniMap Route Relay

A small Go service that relays the active MiniMap route between the iPhone app and the Redmi watch over HTTPS. The hosting platform terminates HTTPS; the Go process listens on `SERVER_PORT` (and falls back to `PORT` for other hosts).

## API

- `GET /health` — public health check. Returns `MINIMAP_ROUTE_RELAY_OK`.
- `POST /pair/start` — watch requests a six-digit code and a private pairing session. The code expires after two minutes.
- `GET /pair/start` — watch checks that session using the `X-MiniMap-Pair-Session` header. Before approval it returns HTTP 202; after approval it returns a watch-scoped credential.
- `POST /pair/claim` — iPhone approves the six-digit code with the master bearer token. Each code is single-use.
- `POST /route` — iPhone publishes the current `minimap_route` JSON.
- `GET /route` — watch reads the latest route.
- `DELETE /route` — either client clears the route.

The iPhone uses `Authorization: Bearer <MINIMAP_RELAY_TOKEN>` for route publishing and pairing approval. The watch never receives that master token: it gets a signed, read-only credential through its private pairing session and uses it for `GET /route`. The master token must contain at least 32 characters. `/route` and its JSON payload remain unchanged. POST uses `Content-Type: application/json` and accepts route JSON produced by MiniMap (polyline6 geometry, origin/destination/current location, and turn steps). Posting `{"type":"minimap_route","available":false}` clears the active route.

The service keeps one route in RAM and expires it after five minutes by default. Set `MINIMAP_ROUTE_TTL_SECONDS` to a value from 60 to 3600. Pairing codes are held in RAM, expire after two minutes, are single-use, and are rate-limited. A restart clears the route and any in-progress pairing codes; already paired watch credentials remain verifiable while the master token stays the same. Requests, route bodies, and credentials are not logged or written to disk.

## Deploy on Botkeep

1. Create a Go application and choose this GitHub repository as its source.
2. Set the Startup command to `go run .`.
3. Add an environment variable named `MINIMAP_RELAY_TOKEN` with a random value of at least 32 characters. Keep it private and configure it only in the iPhone app. The watch pairs using a short code and never needs the master token entered manually.
4. Deploy. Use the HTTPS domain assigned by the host for both clients; do not configure an IP address.

The process reads `.env` from its working directory at startup when that file exists. Keep Botkeep's Working Directory at the repository root (the directory containing `go.mod`) and use `go run .` as the Startup command. Variables already injected into the process environment take precedence over `.env` and are never overwritten. Startup logs only confirm whether `.env` was loaded; they never print variable values. Botkeep provides the process port in `SERVER_PORT`; the server binds to that port on all interfaces. Configure an HTTPS hostname in Botkeep's Domains tab and use that hostname from both clients. Do not hard-code a port or commit the token.

## Local development

```sh
MINIMAP_RELAY_TOKEN=replace-with-a-random-secret-of-32-characters-or-more go run .
```

Run checks with `go test -race ./...`, `go vet ./...`, and `go build ./...`.

## Scope and limits

This relay does not calculate routes, fetch maps, or access phone/watch location. The iPhone app still needs to POST route updates to this API, and the watch needs to GET them using the same relay URL and token. It is a single-instance, in-memory prototype; multiple server instances would need shared storage.
