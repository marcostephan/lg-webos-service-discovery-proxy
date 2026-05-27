# lgtv-sdp

A tiny stand-in for LG's "Service Discovery Platform" servers, so the TV's
home menu stops nagging about ads, tracking, and "not connected" errors —
while leaving network connectivity and time sync intact.

This is a Go rewrite of [wisq/lgtv-sdp][upstream] with extras to keep
modern (webOS 8 / SDP v14) firmware happy.

[upstream]: https://github.com/wisq/lgtv-sdp

## Why this exists

On boot, an LG TV posts to `https://<region>.lgtvsdp.com/rest/sdp/v<N>/initservices`.
The [upstream Ruby project][upstream] documents what happens if the response
never comes: the TV's "Network Connection" check fails and automatic time
sync is disabled. Just blackholing the LG domains has the same effect — you
need a substitute that answers.

This service:

- Replies to `initservices` with a static JSON payload, so requests succeed.
- Sets an `X-Server-Time` header on that reply (the field the upstream
  project identified as the TV's pre-NTP clock source).
- Returns empty-but-200 replies for the other SDP endpoints we saw the
  TV call during startup (`notice`, `eula`, `serverstatus`) so they don't 404.

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

## Install (published image)

Multi-arch images (`linux/amd64`, `linux/arm64`) at
`ghcr.io/marcostephan/lg-webos-service-discovery-proxy`:

```sh
docker pull ghcr.io/marcostephan/lg-webos-service-discovery-proxy:latest
```

Or pin a version: `:v0.1.0`. A compose snippet to drop into your stack:

```yaml
services:
  lgtv-sdp:
    image: ghcr.io/marcostephan/lg-webos-service-discovery-proxy:latest
    container_name: lgtv-sdp
    restart: unless-stopped
    read_only: true
    cap_drop: ["ALL"]
    cap_add: ["NET_BIND_SERVICE"]
    ports:
      - "${BIND_IP}:443:443"
      - "${BIND_IP}:80:80"
```

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

This service is useless unless the TV's DNS lookups for LG's domains return
**your** host instead of LG's real servers. Two things have to be true:

1. The TV must resolve through a DNS server **you control**. If the TV is
   configured (manually or via DHCP) to use `8.8.8.8` / `1.1.1.1` / the ISP's
   resolver, you can't override its lookups and the service can't help. Push
   your local resolver to the TV via DHCP, or hard-code it in the TV's network
   settings.
2. That local resolver must have **override records** for the four LG SDP
   wildcard domains pointing at the host running this service. Anything that
   does authoritative overrides works: UniFi's Custom DNS Records, Pi-hole's
   Local DNS, AdGuard Home's DNS rewrites, dnsmasq's `address=/lge.com/...`,
   PowerDNS, BIND zones — whatever you have.

Add four `A` records:

| Hostname            | Value             |
|---------------------|-------------------|
| `*.lgtvsdp.com`     | `<lgtv-sdp host>` |
| `*.lge.com`         | `<lgtv-sdp host>` |
| `*.lgsmartad.com`   | `<lgtv-sdp host>` |
| `*.lgappstv.com`    | `<lgtv-sdp host>` |

If your resolver doesn't support wildcards, add at minimum:
`ca.lgtvsdp.com` (or `eu.lgtvsdp.com` for EU TVs) and `ngfts.lge.com`. The
TV's `Host` header in our verified requests revealed the rest.

**Verify resolution from a client on the same LAN** before touching the TV:

```sh
nslookup ca.lgtvsdp.com   # must return your <lgtv-sdp host>, not an LG IP
nslookup ngfts.lge.com    # same
```

Then restart the TV. We verified a remote-initiated reboot was enough to
trigger the `initservices` call — a full unplug isn't required.

### UniFi specifically

Settings → Routing → DNS → Custom DNS Records. Add the four wildcards above.
UniFi 4.x supports wildcards; older controllers may not — fall back to the
specific hostnames.

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

The TV hit our server with `Host: ngfts.lge.com` for the thumbnail requests —
confirming the `*.lge.com` wildcard catches those. After we shipped support
for the `v14.0` paths, the TV stopped retrying `initservices` and the country
prompt from the canned reply triggered (so the TV definitely read and acted
on the response body).

## Credits

Original reverse-engineering and reply payload: [wisq/lgtv-sdp][upstream].

## License

[The Unlicense](LICENSE) — public domain, do whatever.
