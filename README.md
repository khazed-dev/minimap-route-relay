# MiniMap Route Relay

A small Go service that relays the active MiniMap route between the iPhone app and the Redmi watch over HTTPS. The hosting platform terminates HTTPS; the Go process listens on `SERVER_PORT` (and falls back to `PORT` for other hosts).

## API

- `GET /health` — public health check. Returns `MINIMAP_ROUTE_RELAY_OK`.
- `POST /route` — iPhone publishes the current `minimap_route` JSON.
- `GET /route` — watch reads the latest route.
- `DELETE /route` — either client clears the route.

All `/route` calls require `Authorization: Bearer <MINIMAP_RELAY_TOKEN>`. The token must contain at least 32 characters. POST uses `Content-Type: application/json` and accepts route JSON produced by MiniMap (polyline6 geometry, origin/destination/current location, and turn steps). Posting `{"type":"minimap_route","available":false}` clears the active route.

The service keeps one route in RAM and expires it after five minutes by default. Set `MINIMAP_ROUTE_TTL_SECONDS` to a value from 60 to 3600. A restart clears the route. Requests and route bodies are not logged or written to disk.

## Deploy on Botkeep

1. Create a Go application and choose this GitHub repository as its source.
2. Set the start command to `go run .` (or build a binary with `go build -o server .` and run it).
3. Add an environment variable named `MINIMAP_RELAY_TOKEN` with a random value of at least 32 characters. Keep the same value private and configure the iPhone publisher and watch reader with it.
4. Deploy. Use the HTTPS domain assigned by the host for both clients; do not configure an IP address.

Botkeep provides the process port in `SERVER_PORT`; the server binds to that port on all interfaces. Configure an HTTPS hostname in Botkeep's Domains tab and use that hostname from both clients. Do not hard-code a port or commit the token.

## Local development

```sh
MINIMAP_RELAY_TOKEN=replace-with-a-random-secret-of-32-characters-or-more go run .
```

Run checks with `go test -race ./...`, `go vet ./...`, and `go build ./...`.

## Scope and limits

This relay does not calculate routes, fetch maps, or access phone/watch location. The iPhone app still needs to POST route updates to this API, and the watch needs to GET them using the same relay URL and token. It is a single-instance, in-memory prototype; multiple server instances would need shared storage.
