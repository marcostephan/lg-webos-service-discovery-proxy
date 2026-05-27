# lgtv-sdp

A tiny stand-in for LG's "Service Discovery Platform" servers, so the TV's
home menu stops nagging about ads, tracking, and "not connected" errors —
while leaving network connectivity and time sync intact.

This is a Go rewrite of [wisq/lgtv-sdp][upstream] with extras to keep
modern (webOS 8 / SDP v14) firmware happy.

[upstream]: https://github.com/wisq/lgtv-sdp

## Why this exists

On boot, an LG TV posts to `https://<region>.lgtvsdp.com/rest/sdp/v<N>/initservices`.
If the response never comes, the TV decides it's offline and refuses to sync time.
If you blackhole the LG domains *without* providing a substitute, you lose
auto time-sync and the "Network Connection" indicator goes red.

This service:

- Replies to `initservices` with a static JSON payload (so the connectivity
  check passes).
- Sets the `X-Server-Time` header, which the TV uses to seed its clock at boot.
- Returns empty-but-200 replies for the other SDP endpoints the TV calls
  during startup (`notice`, `eula`, `serverstatus`) so nothing else 404s.

That's the entire job.

## How it works

```
┌────────┐  DNS: *.lgtvsdp.com → lgtv-sdp host       ┌──────────────────┐
│  TV    │  ───────────────────────────────────────► │   lgtv-sdp       │
│        │  HTTPS POST /rest/sdp/v14.0/initservices  │   (Go, scratch)  │
│        │  ◄─────── 200 + static JSON + X-Server-Time │  ~5 MB binary  │
└────────┘                                           └──────────────────┘
```

The server presents a self-signed cert with SANs for all four LG domains
(`*.lgtvsdp.com`, `*.lge.com`, `*.lgsmartad.com`, `*.lgappstv.com`). The TV
does *not* validate certs at cold boot — it can't, it doesn't know the time
yet — so a freshly-minted self-signed cert is accepted.

## Configuration

Deployment targets live in `.env` (gitignored). Copy the template:

```sh
cp .env.example .env
# edit .env: SSH_HOST, REMOTE_DIR, BIND_IP, TEST_URL
```

`docker-compose.yml` reads `BIND_IP` automatically; `Taskfile.yml` loads the
rest via its `dotenv:` directive.

## Quick start (local dev)

```sh
task run      # listens on :8080 (HTTP) and :8443 (HTTPS)
task test     # unit tests
task check    # fmt + vet + test
```

Hit it:

```sh
curl -ks -i -X POST https://localhost:8443/rest/sdp/v14.0/initservices
```

## Running it for real

The service binds 80 + 443. You almost certainly want a dedicated IP on
your TV's network so those ports stay free on the host's primary address:

```sh
# Example: add 192.168.x.7 alongside the host's existing IP
sudo ip addr add 192.168.x.7/24 dev eth0
```

Then point `docker-compose.yml`'s host-binding at it and:

```sh
task docker:up
task smoke
```

## DNS

You need to make the TV's resolver return your host for the LG SDP domains.
On a UniFi network, add four `Custom DNS Records` (Settings → Routing → DNS):

| Hostname            | Type | Value         |
|---------------------|------|---------------|
| `*.lgtvsdp.com`     | A    | `<your-IP>`   |
| `*.lge.com`         | A    | `<your-IP>`   |
| `*.lgsmartad.com`   | A    | `<your-IP>`   |
| `*.lgappstv.com`    | A    | `<your-IP>`   |

Reboot (power-cycle, not just standby) the TV after the records are in. The
`initservices` call only happens on cold boot.

## Customising the reply

The reply body lives at `replies/initservices.json` and is embedded into the
binary at compile time. The relevant block is `country`:

```json
"country": {
  "code": "AT",
  "threecode": "AUT"
}
```

If you're not in Austria, change this to your ISO 3166-1 alpha-2 / alpha-3
codes before building. The rest of the payload is a cached-from-2017 snapshot
of LG's response and you almost certainly don't need to touch it — the TV
ignores most of it.

## Runtime env vars

| Env var             | Default | Purpose                       |
|---------------------|---------|-------------------------------|
| `LGTV_HTTP_ADDR`    | `:80`   | HTTP listen address           |
| `LGTV_HTTPS_ADDR`   | `:443`  | HTTPS listen address          |

## Tasks

`task` (or `task --list`) shows everything. The interesting ones:

- `task run` — run locally on high ports
- `task check` — fmt + vet + test
- `task docker:up` / `docker:down` / `docker:logs`
- `task deploy` — rsync-via-tar to the vserver and rebuild
- `task smoke` — verify the deployed service end-to-end

## Observed TV requests

What we actually saw an LG webOS 8 TV (firmware-of-the-day, May 2026) emit
on cold boot against this server. Sources are real TV IPs on the IoT LAN.

| Method | Path                                            | Status | Notes                                                  |
|--------|-------------------------------------------------|--------|--------------------------------------------------------|
| POST   | `/rest/sdp/v14.0/initservices`                  | 200    | The critical one — fixes "not connected", seeds clock. |
| GET    | `/rest/sdp/v14.0/notice`                        | 200    | Reads our empty `{"notices":[]}`, satisfied.           |
| GET    | `/rest/sdp/v14.0/eula?voice=Y`                  | 200    | Empty `{"eula":[]}` reply accepted.                    |
| GET    | `/rest/apps/webos8.0/serverstatus/status`       | 200    | Empty `{"status":"ok"}` reply accepted.                |
| GET    | `/fts/gftsDownload.lge?...&func_code=META_THUMBNAIL` | 404 | Home-screen channel thumbnails — **404 is desired**. These would otherwise reach LG's CDN and pull tracked content. |

The TV hit our server with `Host: ngfts.lge.com` for the thumbnail requests
and `*.lgtvsdp.com` for the SDP calls — confirming the wildcard DNS overrides
catch everything that matters.

After the SDP responses landed, the TV's home menu reported "connected" and
the clock synced via `X-Server-Time`. No further LG-domain traffic left
the LAN.

## Footprint

- Binary: ~5 MB (static, scratch base image)
- Image: ~5 MB total
- RAM at rest: a few MB
- No runtime dependencies, no DB, no state.

## Credits

Original reverse-engineering and reply payload: [wisq/lgtv-sdp][upstream].
