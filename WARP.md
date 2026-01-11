# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Common commands

### Build

All commands assume the working directory is this `go-gateway` folder.

**Simple local build (current OS/arch)**

- Go: `go build -o dist/single/warp-gateway .`

On Windows this will produce `dist/single/warp-gateway.exe`. The binary expects its data directory as described in "Runtime data layout" below.

**Cross‑platform builds via scripts**

The `scripts/` directory provides higher‑level build entrypoints that mirror CI:

- PowerShell (Windows/macOS/Linux with PowerShell):
  - Build for the host platform:
    - `pwsh -File scripts/build.ps1 -Target host -Arch amd64`
  - Build multi‑platform matrix (windows/darwin/linux for a single arch):
    - `pwsh -File scripts/build.ps1 -Target all -Arch amd64`
  - Build single, self‑contained binaries into `dist/single` (no backend/resources bundling):
    - `pwsh -File scripts/build-single.ps1 -OS host -Arch amd64`
    - Example cross‑build where supported by CGO/toolchain:
      - `pwsh -File scripts/build-single.ps1 -OS linux -Arch amd64`

- POSIX shell (macOS/Linux):
  - Build for the host platform:
    - `./scripts/build.sh host`
  - Build all supported platforms:
    - `./scripts/build.sh all`
  - Build single binaries into `dist/single`:
    - `OS_LIST=host ARCH_LIST=amd64 ./scripts/build-single.sh`

**Notes**

- The build scripts assume a monorepo layout where this directory lives alongside `backend/` and `resources/` at the repo root. They copy those into the output tree when `IncludeGateway` / `INCLUDE_GATEWAY` is enabled.
- CI builds use `.github/workflows/build.yml`, which compiles `dist/single/warp-gateway_${GOOS}_${GOARCH}[.exe]` and, for Linux, invokes `scripts/package-linux.sh` to create packages.
- Linux builds that use the system tray require GTK/AppIndicator development libraries (see the CI workflow: `libayatana-appindicator3-dev`, `libgtk-3-dev`, plus `rpm` for packaging).

### Run the gateway locally

- Run directly from source:
  - `go run .`
- After startup, the local control UI listens on `http://127.0.0.1:9530` (see `main.go` and `server.go`).
- Logs are written to `go-gateway.log` under the resolved log directory (see "Runtime data layout").

### Tests and basic static checks

There are currently no `*_test.go` files in this module, but the standard Go commands apply once tests are added:

- Run all tests: `go test ./...`
- Run a single test (by name pattern): `go test ./... -run TestName`

For basic static checks using only the Go toolchain:

- `go vet ./...`

No third‑party linter is configured in this repository.

## Runtime data layout and configuration

### Data directory resolution

The runtime data directory is encapsulated by `Paths` in `config.go` and is resolved as follows:

- If `GATEWAY_DATA_DIR` is set, its value is used directly.
- Otherwise, the directory is derived from the executable location and OS:
  - Windows: `<exe_dir>/data`
  - macOS: `${UserConfigDir}/warp-gateway` if available, else `~/Library/Application Support/warp-gateway`, else `<exe_dir>/data`
  - Linux/other: `${UserConfigDir}/warp-gateway` if available, else `~/.config/warp-gateway`, else `<exe_dir>/data`

Within this directory:

- Logs: `<dataDir>/logs` (created on startup)
- Core files (auto‑created if missing or empty, see `ensureDataFiles`):
  - `config.json` (via `LocalConfig` in `config.go`)
  - `gateway_accounts.json` – local account pool snapshot
  - `remote_backend.json` – remote backend activation/device binding metadata
  - `warp_config.json` – overrides for locating the Warp desktop binary
  - `warp_rules_config.json` – locally configured Warp AI rule definitions
  - `selected_rule_ids.json` – IDs of rules currently enabled for injection
  - `inject_rules_enabled.json` – global toggle for whether rule injection is active
  - `warp_settings_backup.json` – backup of selected Warp settings

`main.go` ensures that the data and log directories exist and that all of the above seed files are present before the app starts serving.

### Local configuration (`LocalConfig`)

`LocalConfig` in `config.go` is persisted to `config.json` and holds durable client state such as:

- Device identity and activation: `DeviceID`, `Token`, `ExpiresAt`, `AccountCount`
- Gateway behaviour: `GatewayPort`, `AutoRestartWarp`, `AutoRestartGateway`, `SwitchCount`, `LastUpdated`
- Warp integration: `WarpPath` (user‑specified or auto‑detected path to the Warp desktop binary), `WarpFirebaseAPIKey` (for token refresh flows)

`ensureDeviceID` guarantees a non‑empty, random `DeviceID` at startup; `saveConfig` sets `LastUpdated` on every write.

## High‑level architecture

### Entry point and application orchestrator

- `main.go` is the entry point:
  - Resolves `Paths` (data/log/config/accounts locations).
  - Ensures required directories and seed data files are present.
  - Loads `LocalConfig`, injects a `DeviceID` if needed, applies legacy migrations, and saves it back.
  - Sets up logging (`NewLogStream`, `NewLogger`) with a file sink and in‑memory stream for the UI.
  - Constructs a single `App` instance and starts the HTTP UI server plus the system tray UI.

- `App` in `app.go` is the central coordinator and owns:
  - Process‑wide config (`LocalConfig`) guarded by `cfgMu`.
  - The `GatewayService` instance, its port, and lifecycle (`gatewayMu`).
  - The Warp desktop subprocess (`warpCmd`, via `warpMu`).
  - Remote backend client (`RemoteClient`) and in‑memory account snapshots.
  - Shared logging (`Logger`) and log streaming channel (`LogStream`).

Most cross‑cutting behaviour (starting/stopping the gateway, Warp client, activation flows, account refresh, etc.) is implemented as methods on `*App`.

### Local HTTP API and web UI

- `server.go` exposes a local HTTP API bound to `127.0.0.1:<appPort>`:
  - Serves the embedded UI at `/`.
  - Serves static assets under `/assets/`.
  - Exposes JSON APIs under `/api/...` for:
    - Activation (`/api/activation/*`)
    - Account listing, switching, refresh (`/api/accounts*`)
    - Gateway lifecycle and status (`/api/gateway/*`)
    - Warp desktop lifecycle and configuration (`/api/warp/*`)
    - Streaming and tailing logs (`/api/logs/stream`, `/api/logs/tail`)

- `web/index.html`, `web/app.js`, and `web/styles.css` define a single‑page control panel UI (in Chinese) that:
  - Shows activation status, device ID, and remaining time.
  - Displays the current Warp account, quota usage, and next refresh time.
  - Summarises the account pool and supports manual account switching.
  - Manages the Warp binary path and starts/stops both Warp and the HTTP proxy.
  - Streams the server logs over SSE for live diagnostics.

The Go server serves these assets either directly from the `web/` directory via an embedded filesystem (`embedded.go`) or from copied files in distribution builds.

### Gateway / MITM proxy layer

- `gateway_service.go` defines `GatewayService`, which wraps `github.com/lqqyt2423/go-mitmproxy`:
  - Creates a local HTTP MITM proxy bound to `127.0.0.1:<gatewayPort>`.
  - Configures a custom `http.Client` that uses `warpRoundTripper` for all requests.
  - Installs a `mitmInterceptor` that manages certificates and TLS key logging.
  - Attaches a `WarpAddon` that implements account selection, quota tracking, and error handling on top of proxied requests.

- `gateway_control.go` contains the orchestration logic for starting/stopping the gateway:
  - Validates activation status with the remote backend before starting.
  - Ensures at least one usable account (with secrets) is available, preferring fresh remote data and falling back to local snapshots.
  - Ensures the local MITM root certificate is installed/ready.
  - Starts both the proxy server and the interceptor and waits for the port to be reachable.
  - When successful, updates system proxy settings (via `setSystemProxy`) to route traffic through the gateway and optionally auto‑starts the Warp desktop client.

- `proxy_control.go` and related helpers manage OS‑specific system proxy configuration:
  - Windows: registry and `netsh winhttp` integration.
  - macOS: `networksetup` against the active network service.
  - Linux: `gsettings` or KDE config utilities (`kwriteconfig5/6`) when available.

### Warp desktop integration and credentials

The codebase contains rich, OS‑aware logic to discover, launch, and reconfigure the Warp desktop client:

- `warp_control.go`:
  - Locates the Warp binary on each supported OS using a variety of strategies (`findWarp`, registry lookups on Windows, standard install paths on macOS/Linux, PATH scanning).
  - Normalises user‑provided paths (`setWarpPath`, `resolveWarpExecutable*`).
  - Starts/stops the Warp desktop process and, when the gateway is running, wires Warp through the local proxy via `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`.
  - Locates the Warp data directory (`warpDataDir`) and key data files:
    - `warp.sqlite` – primary app database
    - `dev.warp.Warp-User` – user identity and tokens
    - `dev.warp.Warp-AiApiKeys` – API key storage
  - Updates Warp’s notion of the "current user" (`updateWarpDatabaseCurrentUser`) by editing the SQLite database.
  - Provides per‑platform implementations of `updateWarpCredentials*` to write refreshed tokens and metadata into the OS‑appropriate storage (Linux keyring and JSON files, macOS keychain, Windows DPAPI‑protected files).
  - Offers advanced operations like `resetMachineID`, which rewrites various Warp configuration files/registry keys to randomise client identity.

- `warp_transport.go` defines a custom `http.RoundTripper` used by the gateway when talking to Warp‑owned endpoints:
  - For hosts under `warp.dev`, it uses a special UTLS‑based TLS dialer (`dialWarpUTLS`) and HTTP/2 client with tuned TLS config and key logging.
  - For other hosts it falls back to a more standard `http.Transport`.

- `warp_api.go` implements the GraphQL call that reports per‑account usage/quotas (`WarpUsage`):
  - Makes authenticated requests to `https://app.warp.dev/graphql/v2` with appropriate headers and context metadata.
  - Parses the response to determine quota, used tokens, next refresh, and account type (free vs unlimited), with explicit handling of banned/disabled accounts.

Combined, these components let the gateway transparently rotate through Warp accounts while keeping the official Warp desktop client and its local state consistent.

### Account management and activation backend

- `gateway_accounts.go` and related files (`accounts.go`, `account_memory.go`) implement `GatewayAccountManager`:
  - Maintains an in‑memory view of the account pool (
    local snapshot plus any additional metadata).
  - Keeps track of the current account, total virtual usage, and aggregate quotas.
  - Encodes the account selection policy when an account becomes limited, banned, or errors out.
  - Persists account state back into `gateway_accounts.json`.
  - Triggers Warp credential updates (via `updateWarpCredentialsWithLog`) on account switches and status changes.

- `remote_client.go`, `warp_firebase.go`, and `warp_tokens.go` (not exhaustively listed here) are responsible for remote activation and account provisioning:
  - Talk to a remote backend using `Token`/`DeviceID` from `LocalConfig`.
  - Fetch or refresh account snapshots and secrets.
  - Handle Firebase/ID token flows used in `buildWarpTokenInfo` when only refresh tokens are available.

- `api_handlers.go` wires these pieces into the HTTP API used by the front‑end UI.

### Rule injection and Warp AI context

`gateway_rules.go` manages the optional rule injection system used to provide structured instructions to Warp AI:

- Reads rule enablement state from `inject_rules_enabled.json` and `selected_rule_ids.json` under the gateway data directory.
- Loads rule definitions from two sources:
  - Local `warp_rules_config.json` (`rules` array with `id`, `name`, `content`).
  - Warp’s own SQLite database (`warp.sqlite`), using the `generic_string_objects` table to fetch additional rule content by ID.
- Merges these rules and renders them into human‑readable blocks with Chinese labels and explicit delimiters:
  - `buildRulesBlock` wraps rules between `[[WARP_AI_RULES_BEGIN]]` / `[[WARP_AI_RULES_END]]` markers, ready to be spliced into a prompt.
  - `buildProtobufRulesText` produces a similarly structured string suitable for other consumption paths.

Understanding this flow is important if you need to debug or extend how Warp AI rules are synchronised between the local gateway configuration and the official Warp client.

### Platform integration, tray, and utilities

- `tray.go`, `ui_embedded.go`, `ui_notify.go`, and `logging.go` provide:
  - System tray integration via `github.com/getlantern/systray`.
  - OS‑native notifications for gateway/Warp events where available.
  - Structured logging and live log streaming (used by the `/api/logs/*` endpoints and front‑end log viewer).

- `process_control.go` and `process_control_windows.go` wrap platform‑specific process management primitives:
  - Implement `killProcessByName` and `isProcessRunning` using `taskkill`/`tasklist` on Windows and `pkill`/`pgrep` elsewhere.
  - Hide console windows for child processes on Windows via `SysProcAttr.HideWindow`.

- `cert_control.go`, `net_util.go`, `mitm_interceptor.go`, and `util.go` contain various support utilities for certificate installation, network probing (e.g., port availability checks), and general helpers.

## When modifying this project

- Prefer adding new functionality by extending the existing `App` methods and keeping OS‑specific branches in the dedicated platform files (e.g. `*_windows.go`, `*_other.go`).
- When changing how Warp credentials, rules, or usage are handled, audit the full flow across:
  - `warp_control.go` (file locations and OS integration)
  - `warp_api.go` (remote API semantics)
  - `gateway_accounts.go` and `gateway_rules.go` (selection and rule injection)

This will help maintain consistency between the gateway’s view of accounts, the remote backend, and the installed Warp desktop client.
