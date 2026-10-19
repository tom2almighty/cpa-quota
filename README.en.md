# cpa-quota

[中文](README.md)

CLIProxyAPI (CPA 8.0+) quota plugin: implements the v8 `QuotaProvider` for Claude, Codex, Antigravity, Kimi, xAI, Devin and Meta, with normalized quota windows and an embedded dashboard.

## Overview

The plugin queries each provider on the server and normalizes the result into the v8 `subscription / summary / groups[].buckets[]` shape. CPA serves it through `/v8/management/plugins/cpa-quota/quota`, so the dashboard only renders data and carries no provider-specific quota logic.

The dashboard provides card and weekly reset-calendar views, with provider filters, search, and quota reset.

## Development

Requirements:

- Go 1.25+ with CGO (GCC or Clang)
- Node.js 24+ / pnpm 12+

```bash
cd web && pnpm install && pnpm run dev   # frontend only
go test ./...                            # backend tests
./build.sh                               # build frontend and shared library
```

For frontend development, open `http://localhost:5173/?api_base=http://127.0.0.1:8317` and point it at a running CPA instance.

## Build and deployment

### Install from the plugin store

Once listed in the official plugin store, search for `CPA Quota` in the Management Center. Alternatively, add this repository's store source to CPA:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  store-sources:
    - "https://raw.githubusercontent.com/tom2almighty/cpa-quota/main/plugin-store/registry.json"
  configs:
    cpa-quota:
      enabled: true
```

### Manual install

Download the archive for your platform from [Releases](https://github.com/tom2almighty/cpa-quota/releases) and place `cpa-quota.so` (Linux), `.dylib` (macOS) or `.dll` (Windows) into CPA's `plugins` directory.

### Local packaging

```bash
./scripts/package-release.sh 0.0.2
```

Artifacts are written to `release-dist/` with platform archives and `checksums.txt`.

### Usage

Open `Quota Monitor` from the Management Center plugin list. The page reads credentials and quota through the CPA v8 management API. It reuses the management key saved by cpa-dashboard or the official Management Center, or accepts a manually entered key.

Supported providers:

- Claude (including reset grants)
- Codex (including reset credits)
- Antigravity
- Kimi / Kimi AI
- xAI
- Devin
- Meta

### Docker

Mount CPA's plugin directory:

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/router-for-me/cliproxyapi:latest
    volumes:
      - ./plugins:/app/plugins
      - ./config.yaml:/app/config.yaml
    ports:
      - "8317:8317"
```

Place the shared library in `./plugins` and restart the container.

## License

[AGPL-3.0](LICENSE)
