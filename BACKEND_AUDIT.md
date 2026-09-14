# FTurnPc singbox backend fixes

Base reviewed revision:

- Repository: `LiTarPc/FTurnPc`
- Branch: `singbox`
- Base commit: `ac47187c4619263aae7049d7592ecd277ae7de98`
- Review date: 2026-09-14

This archive contains only files that were changed or added during the backend review. Copy the `backend/` directory over the repository's existing `backend/` directory; files not present in this archive should remain unchanged.

## Architecture preserved

The intended transport chain is preserved:

`system -> fturn-tun -> sing-box protocol -> 127.0.0.1:9000 -> freeturnclient -> TURN -> VPS`

`127.0.0.1:9000` is intentional. FreeTurn remains the transport for all supported sing-box protocols.

`ProfileData.Transport` and `ProfileData.Mode` are separate:

- `transport`: TURN transport used by FreeTurn (`-transport`)
- `mode`: FreeTurn backend relay mode (`-mode`)

When `mode` is absent it is inferred from `sb`:

- TCP: VLESS, VMess, Trojan, Shadowsocks and other TCP proxy types
- UDP: WireGuard, Hysteria/Hysteria2/Hy2, TUIC

An explicit `mode` that conflicts with a known protocol is rejected.

## High-priority fixes

### sing-box configuration

- WireGuard conversion now emits the modern endpoint schema (`address`, not legacy endpoint `local_address`).
- Legacy WireGuard outbounds received in ready-made `sb` JSON are migrated to a WireGuard endpoint.
- Supplied proxy targets are normalized to `127.0.0.1:9000` while preserving the original hostname as TLS `server_name` when needed.
- VLESS/Trojan TLS no longer loses implicit SNI when the server is replaced with localhost.
- Hysteria2/TUIC always receive TLS configuration and keep the original SNI.
- SIP002 Shadowsocks Base64 userinfo is decoded correctly, including the legacy whole-authority form.
- `hy2://` is recognized by config detection.
- Invalid URI ports/credentials/transports are rejected early.
- Untagged single proxy entries receive a real `proxy` tag rather than producing dangling `route.final`/DNS detour references.
- A user object cannot hijack the reserved `direct` tag with a non-direct outbound.
- RU bypass never references a missing `geoip-ru` rule-set. `.ru/.su/.xn--p1ai` inline rules remain available even without the SRS file.
- Empty optional `dns.rules` / `route.rule_set` are omitted instead of serialized as `null`.
- TUN keeps `auto_route`, `strict_route`, `dns_mode=hijack`; Linux also enables `auto_redirect`.
- All executable basenames that `getFreeturnPath()` can select are bypassed from TUN, preventing the FreeTurn process from routing back through itself.

### FreeTurn mode and process lifecycle

- FreeTurn now receives explicit `-mode tcp` or `-mode udp` based on the proxy protocol.
- `-mode` is not confused with `-transport`.
- Invalid sing-box configuration is checked before FreeTurn is started.
- Generated sing-box config is per-session, mode `0600`, and removed at teardown instead of reusing permanent `config.json` containing credentials.
- FreeTurn is attached to the Windows Job Object too, not only sing-box.
- FreeTurn child process wait uses the captured command instead of mutable engine state.
- Production FreeTurn launch no longer enables `-debug` unconditionally.
- Secrets in launch arguments remain redacted in logs.

### SingboxTun lifecycle

- stdout and stderr are consumed independently; `io.MultiReader(stdout, stderr)` starvation is removed.
- `cmd.Wait()` starts immediately after process start, including failures before readiness.
- `Start()` no longer holds the state mutex through config checking/readiness waiting.
- `Stop()` can cancel an in-progress startup and cannot race a second Start into the first startup.
- Parent/session context is respected instead of using a detached `context.Background()` process lifetime.
- Runtime sing-box exit invokes the engine failure path; FreeTurn is torn down and Orchestrator reconnects instead of leaving a transport-only session that appears healthy.
- Log scanner limit is raised to 2 MiB; emitted/logged lines are bounded to avoid UI/log amplification.

### FreeTurn log parser

- `activeConnectionCount` is a readiness signal only when the parsed value is `>= 1`; `activeConnectionCount=0` no longer starts sing-box.
- Empty stream IDs are ignored.
- Error/event log lines sent to the UI are bounded.
- Duplicate sing-box startup from repeated FreeTurn readiness lines is prevented with `sbStarting`/`sbApplied` state.
- Sing-box startup failure tears down the FreeTurn session instead of leaving it alive waiting indefinitely for another log line.
- Delayed NAT diagnostics respect session cancellation before emitting stale events.

### Orchestrator

- Start/Stop/reconnect transitions are serialized.
- Session contexts derive from the application context.
- Reconnect sleeps are context-aware.
- Network polling uses a ticker instead of allocating `time.After` on each loop.
- Log output is restored before the session log file is closed.
- Configuration/log directories are restricted to `0700`; profiles/configs remain `0600`.

### Core updater

- Automatic core update only accepts release assets from `samosvalishe/free-turn-proxy`.
- The release asset SHA-256 from GitHub release metadata is verified before installation.
- Downloads and extracted executables have hard size limits.
- ZIP extraction only accepts known FreeTurn client filenames; it never falls back to the first arbitrary executable in an archive.
- Windows PE, Linux ELF and macOS Mach-O/FAT headers are validated.
- Replacement is staged in the target directory and uses backup/rollback semantics.
- Symlink targets are rejected for automatic replacement.
- Core asset selection is exact for OS + architecture instead of a soft cross-architecture fallback.

### Windows network monitor

- `cmd /c route print 0.0.0.0` is no longer spawned every monitor tick.
- Default route/gateway/interface are read from the already-used IP Helper `GetIpForwardTable` API.
- Network-change state compares route availability, gateway and interface index.

## Tests added/replaced

The test suite covers:

- config type detection;
- VLESS/Trojan/Shadowsocks/Hysteria2/Hy2/TUIC parsing;
- TLS/SNI preservation;
- modern and legacy WireGuard conversion;
- tag/reference integrity;
- RU rule-set integrity;
- FreeTurn process bypass names;
- FreeTurn TCP/UDP mode mapping and argument generation;
- FreeTurn readiness parsing (`activeConnectionCount=0` vs `>=1`);
- SingboxTun startup, stdout/stderr readiness, early exit, Wait, cancellation, concurrent Start/Stop, runtime crash callback and long log lines;
- updater asset selection, trusted URL validation, SHA-256 parsing, ZIP executable selection, size limiting, executable magic and atomic replacement;
- a build-tagged real `sing-box check` integration matrix.

## Local validation performed

The changed code was compiled/tested in a standalone backend harness with Wails/platform stubs for dependencies outside the edited set.

Successful checks:

```text
go test ./backend -count=1
go vet ./backend
go test -race ./backend -count=1
GOOS=windows GOARCH=amd64 go test -c ./backend
GOOS=darwin  GOARCH=amd64 go test -c ./backend
GOOS=linux   GOARCH=amd64 go test -c ./backend
go test -tags=integration ./backend -run '^$'
```

The default suite currently contains more than 50 focused tests in the edited test files.

### Real sing-box schema test

`backend/singbox_schema_integration_test.go` is included under the `integration` build tag. Run it against the exact sing-box binary shipped with the application:

Windows PowerShell:

```powershell
$env:SING_BOX_BIN="C:\path\to\sing-box.exe"
$env:SING_BOX_EXPECTED_VERSION="1.14."
go test -tags=integration ./backend -run TestSingBoxSchema -v
```

Linux/macOS:

```bash
SING_BOX_BIN=/path/to/sing-box \
SING_BOX_EXPECTED_VERSION=1.14. \
go test -tags=integration ./backend -run TestSingBoxSchema -v
```

The integration test was compiled here, but the actual release binary could not be downloaded in this execution container because outbound DNS/network access from the container is disabled. Therefore do not treat the local checks as a substitute for running this command with the release binary before shipping.

## Remaining known limitations / follow-up work

1. **No true OS-level kill switch.** `strict_route` protects traffic while sing-box is alive and a sing-box crash now immediately fails the session, kills FreeTurn and triggers reconnect. There is still a small fail-open interval between TUN removal and teardown. A strict no-leak guarantee requires a persistent Windows WFP/Firewall and Linux/macOS firewall policy that remains active independently of sing-box.
2. **Shadowsocks UDP semantics.** The current FreeTurn mapping uses TCP mode for Shadowsocks, matching the TCP-forwarder model. Native Shadowsocks UDP needs an explicit UoT/separate listener/server design if it must be supported.
3. **Legacy platform WireGuard code remains in the repository.** Linux/macOS legacy WG implementation and WireGuard dependencies are currently dead for the new engine path but were not deleted in this patch to avoid turning this backend correctness pass into a platform migration/removal patch.
4. **NAT type diagnostics are approximate.** The existing STUN helper is informational and is not used to establish tunnel security. A standards-complete NAT classifier should reuse one bound UDP socket and validate STUN transaction IDs.
5. **Updater trust root is still GitHub release metadata.** SHA-256 protects integrity and accidental/corrupt downloads, but it is not an independent publisher signature. A pinned signing key/minisign/cosign-style release signature would provide stronger supply-chain security.
6. A full Wails application build against the complete repository should still be run after copying these files; this environment cannot write/clone the connected repository directly.

## Files in this archive

See `CHANGED_FILES.txt`. No dependency was added to `go.mod`; all new implementation code uses the Go standard library and existing project dependencies.
