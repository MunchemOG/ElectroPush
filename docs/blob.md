# blob: what was removed, and how to get it back

epsh is a fork of Pusher (<https://github.com/PzmuV1517/Pusher>). Pusher has a
set of features built around **blob**, a private FTC path-following and
motion-control library by the same author (`PzmuV1517/blob` on GitHub). This
fork has no access to that repository, so every blob feature was removed when
Pusher v1.3.3 (upstream commit `7d11705`) was merged in.

This file records what those features were so they can be restored if access is
ever granted.

## What the features did

### Hidden behind an unlock

None of this showed up on a normal install. In `epsh settings`, typing the
Konami code (`↑ ↑ ↓ ↓ ← → ← → b a enter`) unlocked the install
(`internal/feature`). That showed the **blob library** row (under "Extras") and
the `epsh visualiser` command. Most actions then also needed a GitHub token
that could read `PzmuV1517/blob`.

### GitHub access (`internal/ghauth`)

- Looked for a token in `GH_TOKEN`, then the `gh` CLI, then git's credential
  helper, or took one typed into the "GitHub token" menu entry.
- Checked it against the private repo and cached the result for a week in
  `~/.config/epsh/credentials`.

### Managing the library (`internal/blobrel`, `internal/blobdep`, `internal/tui/blob.go`)

Under `epsh settings` → blob library:

- **Add blob to the project.** Downloaded the release `.aar` into
  `TeamCode/libs/`, added the dependency to `TeamCode/build.gradle`, and added
  `TeamCode/libs/*.aar` to `.gitignore`. It refused if the `.aar` was already
  committed, because blob is private and team repos are usually public.
- **Build variant.** Switched between `blob-competition` (no recording code)
  and `blob-dev` (can record traces and follower logs).
- **Release branch.** blob tagged branch builds like `v1.8.0-RSTController.1`.
  The text before the first dot was the branch, and the project could follow
  one. Stored as `blob_branch` in the epsh config, default `main`.
- **Version.** Bumped the project to the newest release on the followed branch.
- Changing the library forced a full install on the next deploy rather than a
  delta or an Extreme reload.

### Update notices (`internal/updates/blob.go`, `cmd/blobupdate.go`)

If the project used blob, a deploy checked GitHub in the background and printed
"blob X is available; this project uses Y" twice: once before the build and
once at the end. It said nothing without access or without blob in the project.

### Path visualiser (`epsh visualiser` / `vis`, `internal/pathtrace`, `internal/visual`)

- Pulled a path trace off the robot from `/sdcard/FIRST/pusher-traces`. This
  fork had renamed that to `epsh-traces`, but blob writes to `pusher-traces`.
- Rendered an HTML page of the whole autonomous: every curve coloured by
  modelled speed, segments labelled with the `case` they came from (blob
  captured a stack trace per path commit, and epsh mapped it back to source),
  and an estimated duration next to the measured one.
- Flags: `--file`, `--out/-o`, `--no-open`, `--project`, `--top-speed`,
  `--accel`, `--decel`, `--lat-accel`.
- Also available from settings → blob library → **Recorded runs**, and as a
  demo in `epsh dev`.
- Recording needed `blob-dev` with `BlobParams.recordTrace = true` and
  `blob.saveTrace()` in `stop()`.

### Follower logs (`internal/follower`, `internal/tui/follower.go`)

- blob-dev could log every control loop to `/sdcard/FIRST/blob-follower`
  (`BlobParams.recordFollower = true`), one CSV per OpMode run.
- Listed under settings → blob library → **Follower logs**, or opened with
  `epsh visualiser --file follower-….csv`.
- The page showed:
  - reference against measurement per axis (forward, left, turn)
  - turn commanded against delivered, recovered by running blob's wheel mix
    backwards, with saturated loops shaded
  - volts per axis against the pack, with the kS feedforward shaded
  - the loop period distribution and the fastest `tauClosedLoop` it supports
  - the four wheel commands, and heading error over time
- Its purpose was to tell "the robot is fighting its mechanics" apart from "the
  code doesn't do what we think".

## What stayed

These do not depend on blob, so they stayed:

- **Power monitor** (`epsh power`) and **loop profiler** (`epsh profile`).
  epsh generates its own recorder file into the project.
- Mentions of blob in Epsh Extreme's lists of libraries that resolve classes
  reflectively. Those describe general library behaviour.

## Getting it back

Everything is in upstream Pusher at `7d11705` (tag `v1.3.3`). Fetch it with
`git fetch pusher`, then restore the packages listed above with
`git checkout 7d11705 -- <path>` and run them through the same rename used for
the rest of the fork:

- `pusher` → `epsh`, `Pusher` → `Epsh`, `PUSHER` → `EPSH`
- `github.com/andreibanu/pusher` → `github.com/MunchemOG/ElectroPush`
- keep `PzmuV1517/blob` and `/sdcard/FIRST/pusher-traces` as they are, since
  those are blob's names, not Pusher's

Then re-wire the call sites:

- the settings menu row and screens in `internal/tui/settings.go`
- `announceBlob` / `updates.WatchBlob` in `cmd/push.go`
- the visualiser in `cmd/root.go`, `cmd/help.go` and `internal/tui/dev.go`

Future merges from Pusher will keep bringing blob changes back in. Drop them
during the merge.
