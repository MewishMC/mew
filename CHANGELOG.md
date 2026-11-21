# Changelog

## 3.1.3

- More download links: paste a MEGA, Google Drive or Modrinth link into the Pack
  Porter, and a MEGA, Google Drive or MediaFire folder link into the Folder
  Porter. No account or key is needed for any of them.
- Modrinth mods and datapacks are refused, because Bedrock can use neither.

## 3.1.1

- Auto-updates: MEW now fetches the new build, swaps it in and restarts itself
  through the small `update.exe` helper shipped in each release, instead of
  downloading a copy to Downloads and running it by hand.

## 3.1.0

- Pack Viewer: the pack's own sky shows again. A leftover `skySlider`
  reference threw during loading, so the sky step never ran and the viewer
  always fell back to the default gradient. Removed the Viewer Background and
  Default Sky Pack settings, which no longer did anything useful.
- Pack Viewer: dragging left and right now turns the view the same way the
  pointer moves; up and down were already correct.
- Recolor editor: saving a texture over an existing one in an installed pack
  asks before overwriting again (the check was rejected silently).
- Hardening from the audit: untrusted archive/download names, unbounded
  downloads, startup event crashes, a custom pack-cache folder, update checks,
  and fully failed port batches (now report an error).

---

Short, append-only summary of the audit's own changes. Details and IDs are in
ISSUES.md; the commands used are in NOTES.md.

## Feature testing pass

Every bound backend feature is now driven end to end by tests (porting, batch
porting, URL porting, sky packs, animated inventories, the manifest tool, the
recolor tool, the pack viewer, per-pack skins, settings/data folders, exports,
updates and MediaFire). Seven defects fell out of that:

- **Startup-context crashes (high)** — a progress event or a batch port before
  startup finished killed the process: Wails exits on an invalid context, and
  `context.WithCancel(nil)` panics. All events go through one guarded helper and
  batch ports fall back to a background context.
- **Wrong cache folder** — the cache browser and cache export used the default
  Minecraft cache even when a custom `packCachePath` was set.
- **Update prompts** — an older release tag offered a "downgrade", versions were
  compared as text, and the installer download had no timeout.
- **Download names** — a URL without a file name produced a file named after the
  host, and RFC 5987 `filename*=` names kept their `UTF-8''` prefix.
- **Silent batch failures** — `PortFolder`/`PortLocalArchive` returned success
  when every pack failed.

Behaviour changes worth knowing: a fully failed batch now returns an error
(the partial case still succeeds), and `CheckForUpdate` only reports a newer
version.

## Security

- **Path traversal (critical)** — a pack downloaded from a URL no longer gets to
  choose where it is written: every file name from a server, a
  `Content-Disposition` header or a MediaFire folder listing is reduced to a
  plain base name (`safeArchiveName`, `mediafire.sanitizeFilename`).
- **XSS in the recolor tool (high)** — pack names/descriptions from an untrusted
  manifest are escaped with `parseBedrockCodes` instead of raw `v-html`.
- **Unbounded downloads (high)** — downloads are capped at 512 MiB and the
  MediaFire API response at 8 MiB; the folder-listing loop is capped at 100
  chunks and its folder key is query-escaped.
- **Update path (medium)** — `DownloadUpdate` only accepts https GitHub release
  URLs and `InstallUpdate` only runs a `.exe`/`.msi` sitting in the user's
  Downloads folder. *Behaviour change:* a self-hosted update mirror is rejected.
- **Session folder containment (low)** — `GetImages` refuses a folder that
  escapes the recolor session.
- **Frontend dependencies** — `npm audit fix`: 9 advisories (8 high) → 4 high.
  The remaining four are the picomatch ReDoS advisory, which has no published
  fixed version; see SEC-3b in ISSUES.md.

## Fixed behaviour

- Exported `.mcpack` files use slash-separated zip entries (they used to get
  Windows backslashes, which Bedrock would not open).
- Exports fall back to a writable folder everywhere, not just in the recolor
  export, so porting no longer fails when MEW runs from Program Files.
- Exporting over an existing **read-only** `.mcpack` (the other cause of
  `failed to create mcpack: open image (1).mcpack: Access is denied`) now clears
  the attribute and overwrites it instead of failing on every retry.
- Export errors name the folder they were writing to.
- Replacing a texture now refreshes its sidebar thumbnail.
- The porting flow's dead `deleteMcpack && autoImport` branch is gone.
- Recent Packs stores the written `.mcpack` path instead of a bare file name.
- JPEG pack icons are served as `image/jpeg`.
- Nested `ui/**.json` files can be read back (the guard rejected any separator).
- Manifest uploads use a unique temp file; export walks no longer leak handles.
- Settings, the folder-port cancel func and the Discord stop channel are safe
  against the concurrent calls Wails makes; a `null` settings payload no longer
  installs a nil map.
- Pruning keeps sessions whose metadata has no `updatedAt`; 8-digit hex colours
  are parsed correctly; `sampleBilinear` no longer divides by zero.
- Home/Recent Packs unregister their `recent-packs-changed` handlers on unmount.

## Tooling

- CI runs `go vet ./...` and `go test ./...` before building (it only built).
- 17 new tests (filename sanitisation/traversal, zip export and read-only
  overwrite, output-folder fallback, thumbnail cache, settings concurrency,
  session pruning, colour parsing, icon mime type).
- The vanilla-mirror tests skip, instead of failing, when the mirror is
  unreachable, so an offline machine does not go red.

## Not changed (with reasons in ISSUES.md)

- picomatch advisories (no fixed release exists; only a vite major downgrade).
- Unvalidated pack names in read-only endpoints (no privilege boundary; writes
  are all validated).
- Always-on `debug.log` writes, the CRLF/gofmt noise, the unreferenced
  `GetPackMenu*` bindings, and the upstream repo URL in the README.
