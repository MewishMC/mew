# ISSUES

Audit of MEW (Wails Go backend + Vue 3 frontend). Severity: critical / high /
medium / low. Status: `open`, `fixed`, `won't-fix` (with reason).

Baseline before any change: `go build`, `go vet` and `go test ./...` all passed
(48 tests, 7 skipped); frontend build passed; `npm audit` reported 9
vulnerabilities (8 high, 1 moderate). See NOTES.md for the exact commands.

Summary: 37 findings — 1 critical, 7 high, 14 medium, 15 low.
**28 fixed, 9 won't-fix** (every won't-fix has its reason below).
The second full pass added the "Pass 2" and TEST/CFG entries; the third pass
found nothing new, and every fix was re-checked with build + vet + test.

## Security

| ID | File:line | Sev | Issue | Status |
| --- | --- | --- | --- | --- |
| SEC-1 | internal/mediafire/mediafire.go (`extractFilename`), porting.go (`writeMcpack`, `portRarPack`, `processPack`) | critical | A pack downloaded from a URL had its server-supplied file name joined straight onto the temp and output folders, so `Content-Disposition: filename="..\..\..\Startup\evil.zip"` wrote outside them — remote-controlled arbitrary file write. | **fixed** — names reduced to a base name (`safeArchiveName` in fileutil.go, `sanitizeFilename` in the mediafire package, percent-decoded first); `porting_test.go`, `mediafire_test.go` |
| SEC-2 | frontend/src/components/tools/RecolorTool.vue:286,325 | high | `v-html="p.name"` rendered a pack's manifest name unescaped; a pack is untrusted input, so its name could run script in the Wails webview, where every bound Go method (exec, delete, download) is reachable. | **fixed** — routed through `parseBedrockCodes` like the other pack lists |
| SEC-3 | frontend/package-lock.json | high | `npm audit`: 8 high, 1 moderate (vue XSS, nanoid, picomatch ReDoS via vite/fdir/tinyglobby, postcss, source-map-js). | **fixed (partial)** — `npm audit fix` takes it to 4 high; see SEC-3b |
| SEC-3b | frontend/package-lock.json | high | The remaining 4 highs are all the picomatch ReDoS advisory, pulled in by vite 7's fdir/tinyglobby. | **won't-fix** — picomatch 4.0.7 is the newest published version, so no upgrade fixes it; `npm audit fix --force` only offers a *downgrade* of vite to 6.x, which would break the project's `vite ^7` contract. Reachable only from the local build tooling, not from the shipped app bundle. Re-check when picomatch 4.0.8 ships. |
| SEC-4 | internal/mediafire/mediafire.go | high | `io.ReadAll` on a caller-supplied URL body with no cap: a hostile server could exhaust memory and kill the app. | **fixed** — `readAllLimited`, 512 MiB for downloads, 8 MiB for API responses |
| SEC-5 | internal/mediafire/mediafire.go (`ExtractMediaFireFolderLinks`) | medium | The folder key from a pasted URL was spliced into the API query unescaped (query injection) and the chunk loop had no cap, so a server answering `more_chunks: yes` forever kept the app paginating. HTTP status was not checked. | **fixed** — `url.QueryEscape`, `maxFolderChunks`, status check |
| SEC-6 | packs.go (`GetImages`) | low | `folder` was joined onto the session dir with no containment check. | **fixed** — `safeRelPath` |
| SEC-7 | updates.go (`DownloadUpdate`, `InstallUpdate`) | medium | `InstallUpdate` executed whatever path it was given and `DownloadUpdate` fetched any URL, so any UI script-execution bug escalated to arbitrary code execution. | **fixed** — downloads only from GitHub release hosts over https; the installer must be a `.exe`/`.msi` in the user's Downloads folder. **Behaviour change:** a self-hosted update mirror would now be rejected (note in CHANGELOG). |
| SEC-8 | packs.go, manifest.go (read-only pack endpoints) | low | `GetPackPreviewInfo`, `GetPackSkyTextures`, `GetPackItemTextures`, `GetPackMenuFiles` etc. accept a pack name with no `..` guard. | **won't-fix** — every *write* path already validates (`DeleteInstalledPack`, `getPackRoot`, `packDirFor`, `SetResourcePacksPath`); these only read image/json data the user already has, from a locally-supplied name, with no privilege boundary crossed. Fixing it means changing ~10 signatures; recorded here instead of churning the code. |

## Bugs

| ID | File:line | Sev | Issue | Status |
| --- | --- | --- | --- | --- |
| BUG-1 | fileutil.go (`createZipFromFolder`) | high | Exported packs got Windows backslash zip entry names (`filepath.Rel` result used raw), which Bedrock and POSIX unzip will not open. | **fixed** — `filepath.ToSlash`; covered by `TestCreateZipFromFolder` |
| BUG-2 | porting.go:300,396,700 | high | Unreachable `if deleteMcpack && autoImport` inside the `else` of the same condition (three copies), so the intended temp-dir path could never run. | **fixed** — removed |
| BUG-3 | settings.go (`getOutputDir`), porting.go, sky.go, animator.go | high | Only `ExportPack` resolved the output dir to a writable folder. Installed under Program Files, porting/sky/animator exports failed with "access is denied" while recoloring worked. | **fixed** — the fallback now lives in `getOutputDir` (memoised per configured value) |
| BUG-4 | fileutil.go:159 | medium | `defer file.Close()` inside the `WalkFunc` held one handle per file for the whole walk. | **fixed** |
| BUG-5 | recolor.go (`GetPackThumb`, `SavePackTexture`) | medium | Thumbnail cache keyed by the raw `relPath` but evicted with the cleaned path, so a replaced texture kept its old thumbnail. | **fixed** — shared `thumbCacheKey`; `TestGetPackThumbCacheInvalidatedBySave` |
| BUG-6 | settings.go | medium | `a.settings` is a plain map shared with Wails goroutines: `GetSettings` mutated it, `SaveSettings` replaced it, `getResourcePacksPath` wrote it while others read it — a concurrent map write panics the app. | **fixed** — `settingsMu`; `GetSettings` returns a snapshot; `TestSettingsConcurrentAccess` |
| BUG-7 | settings.go (`SaveSettings`) | medium | A `null` settings payload installed a nil map; the next write (`SetResourcePacksPath`, the levi fix-up) panicked. | **fixed** |
| BUG-8 | porting.go (`cancelFolder`) | medium | `CancelPortFolder` read the cancel func while a port operation wrote it. | **fixed** — `cancelMu` + `setCancelFolder` |
| BUG-9 | discord.go (`stopDiscordRPC`, `discordLoop`) | medium | `discordStop` was written by shutdown and read by the loop unlocked, and the loop kept polling for up to 5s after shutdown. | **fixed** — channel created once in `NewApp`, closed via `sync.Once`, loop waits on it |
| BUG-10 | updates.go (`DownloadUpdate`) | medium | The previously downloaded `mew.exe` was deleted before the new download started, so a failure left the user with neither file. | **fixed** — the old file is only removed once the new one has arrived |
| BUG-11 | porting.go (`PortPack`) | medium | The Recent Packs entry stored the bare file name instead of the written `.mcpack`, so the UI could not open it. | **fixed** |
| BUG-12 | packs.go (`GetPackMenuContent`) | medium | The guard rejected any separator, but `GetPackMenuFiles` lists nested `ui/**.json`, so every nested file it listed failed to load. | **fixed** — `safeRelPath` |
| BUG-13 | packs.go (`GetInstalledPacksDetailed`) | low | `pack_icon.jpg` bytes were served as `data:image/png`. | **fixed** — `readImageAsDataURI`; `TestGetInstalledPacksDetailedKeepsIconMime` |
| BUG-14 | manifest.go (`GetManifestFromUpload`) | low | Fixed temp path `%TEMP%/mew-manifest-pack.zip`: two uploads collide. | **fixed** — `os.CreateTemp` |
| BUG-15 | animator.go (`mergeAnimIntoTarget`) | low | Returned `true` even when nothing was copied, so a merge into an unwritable pack counted as success. | **fixed** |
| BUG-16 | porting.go:520,643 | low | `copyFile` errors into `Java/` were swallowed. | **won't-fix** — the loop already logs the failure through `log.Printf`/`logError` and continues by design (one bad pack must not abort a batch); the log line is the user-visible report. Making it fatal would change batch semantics. |
| BUG-17 | sky.go (`sampleBilinear`) | low | `% int(srcW)` divides by zero for a zero-width image, panicking the backend. | **fixed** |
| BUG-18 | sessions.go (`pruneStaleSessions`) | low | A session whose metadata had no `updatedAt` was treated as epoch-old and deleted. | **fixed** — dir mtime fallback; `TestPruneStaleSessionsKeepsSessionWithoutTimestamp` |
| BUG-19 | animator.go (`parseHexColor`) | low | An 8-digit `#RRGGBBAA` was shifted into `RRGGBB`, producing the wrong colour. | **fixed**, `TestParseHexColor` |
| BUG-20 | errlog.go (`getErrLogDir`) | low | Lazily cached package global written from several goroutines. | **fixed** — `errLogDirMu` |

## Pass 2 (second full sweep) — frontend

| ID | File:line | Sev | Issue | Status |
| --- | --- | --- | --- | --- |
| FE-1 | frontend/src/components/pages/HomePage.vue:34, RecentPacksPage.vue:47 | medium | Both pages are mounted with `v-if`, so every visit adds another `EventsOn('recent-packs-changed')` listener that is never removed; listeners and reloads pile up over a session. Neither page cleared `isMounted` on unmount either. | **fixed** — unsubscribe stored and called from `onUnmounted` |
| FE-2 | frontend/src/utils/playerSkin.js | low | `sentTex` grows one entry per rendered texture set for the life of the window. | **won't-fix** — bounded in practice (one id per distinct player loadout per session, ~50 bytes each) and the ids must stay stable for the worker; evicting them would re-send textures the worker already holds. |
| FE-3 | frontend/src/utils/hue.js (`upscaleNearestNeighbor`) | low | The promise never settles if the image fails to load (no `onerror`). | **won't-fix** — every caller passes a data URI produced by a canvas in the same session, so the load cannot fail; adding an error path would change what callers receive for no reachable case. |
| TEST-1 | jsonui_vanilla_test.go | medium | Three tests download ~100 files from the public vanilla mirror on every run, so the suite fails on an offline machine and the new CI test step would go red for a network blip. | **fixed** — `requireVanillaMirror` skips with a clear message only when the mirror is unreachable; assertions unchanged when online |

## Dead code / docs / config

| ID | File:line | Sev | Issue | Status |
| --- | --- | --- | --- | --- |
| DEAD-1 | packs.go (`GetPackMenuFiles`, `GetPackMenuContent`) | low | Bound to the frontend but never called from `frontend/src`. | **won't-fix** — both are Wails API surface; removing them is a public-API change with no user-visible benefit, and BUG-12 made them correct. Listed here so the next reviewer knows they are unreferenced. |
| CFG-1 | .github/workflows/dev.yml | medium | Neither workflow ran `go test`/`go vet`, so the Go suite never ran in CI. | **fixed** — a "Vet and test" step now runs before the build |
| CFG-2 | README.md:22, .github/workflows, updates.go | low | Update/download links point at `KyarottoOwO/mew`; this working copy is `mewishMC/mew`. | **won't-fix** — owner/product decision; changing release URLs would break the updater for existing users |
| FMT-1 | repo-wide | low | `gofmt -l` flags 22 files because the working tree has CRLF endings and `.gitattributes` only marks `*.uidx` as binary. | **won't-fix** — a repository-wide renormalisation commit would touch every file and bury the audit diff; the committed content is gofmt-clean (verified by stripping CR, see NOTES.md) |
| BUG-21 | errlog.go (`debugLog` via `logDebug`) | low | Every `logDebug` call appends to `debug.log` on disk even without `--debug` (hot paths: per texture, per file). | **won't-fix** — `debug.log` is a deliberate always-on diagnostic trail (the Logs folder is documented in the debug banner); gating it is a product decision, and it is I/O-buffered by the OS rather than a correctness problem. |

## Tests added

| Test | Guards |
| --- | --- |
| `TestSafeArchiveName`, `TestSafeArchiveNameCannotEscape`, `TestWriteMcpackIgnoresTraversalName`, `TestUnzipRejectsTraversal` | SEC-1 |
| `TestExtractFilenameSanitizes`, `TestSanitizeFilenameRejectsSeparators` | SEC-1 |
| `TestCreateZipFromFolder` | BUG-1, BUG-4 |
| `TestGetPackThumbCacheInvalidatedBySave` | BUG-5 |
| `TestSettingsConcurrentAccess` | BUG-6 |
| `TestPruneStaleSessionsKeepsSessionWithoutTimestamp`, `TestPruneStaleSessionsDropsOldSessions` | BUG-18 |
| `TestParseHexColor` | BUG-19 |
| `TestGetInstalledPacksDetailedKeepsIconMime` | BUG-13 |
