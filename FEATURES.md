# Epsh features

Everything epsh (ElectroPush) does today, grouped by what you would use it for.
The [README](README.md) explains the reasoning and measurements behind each
one; this is the inventory.

## Deploying

### `epsh` — build and deploy

The default command. Run it from anywhere inside an FTC project.

- **USB first.** If a hub is attached over USB, epsh deploys over it and leaves
  your Wi-Fi alone (**Prefer USB when attached** in settings).
- **Otherwise it hops networks for you.** It builds first, joins the robot's
  Wi-Fi, deploys, and puts you back on the network you started on (**Return to
  previous Wi-Fi**).
- **It looks for the hub while the build runs.** A background scan means a hub
  that was just switched on is already in the OS's network list by the time the
  join happens. Scanning stops before the transfer so it doesn't slow it down.
  If the join fails, epsh says whether it ever saw the hub's network, and
  retries once if it did. `EPSH_NO_SCAN=1` turns scanning off.
- **Dropped wireless ADB connections are recovered** in the middle of a deploy
  instead of failing it.
- **It finds the robot on your own network** when the relay is set up: first
  where it was last seen, then the hub's access point, then a sweep of the
  local subnet. The sweep never walks anything larger than a home-sized
  subnet, and asks a device what it is before treating it as a robot.
- **It reports what the deploy overwrote.** With **Dashboard tuning check** on,
  epsh reads FtcDashboard or Panels before the deploy and tells you afterwards
  which tuned values the deploy replaced with the ones in your code.
- **It warns on every deploy** while the power monitor or loop profiler is
  installed, because both cost loop time and shouldn't go into a match.
- `--ignore-warnings` carries on past a check that would otherwise stop the
  command (this flag works on every command).

### Deploy speed

All under `epsh settings` → **Deploy speed**. Every option falls back to a
normal install if something unexpected happens.

| Setting | What it does | Default |
|---|---|---|
| Send only changed parts | Keeps the APK on the hub in chunks under `/data/local/tmp/epsh`, which survives reboots, and sends only the chunks that differ. Measured at 0.6 MB instead of 74 MB for a one-line change. The rebuilt APK is checksummed on the hub before it is installed. | on |
| Skip install when unchanged | Does nothing if the robot already has this build. | on |
| Stream the install | Writes the APK straight into an install session instead of copying it to a temporary file first, which halves what gets written on the robot. | on |
| Store native libraries uncompressed | Stops the install from extracting 20 MB+ of libraries, at the cost of a bigger APK. It also makes delta transfer far more effective. | off |
| Install only changed splits | For a project that builds a base APK plus a feature module, installs only the part that changed. | off |

**Gradle threads** sets how many workers the build uses.

### `epsh slim` — shrink the APK

Drops the native libraries for the CPU architecture your hub doesn't use, about
10 MB of a stock FTC APK. It asks the connected hub which architecture it runs
rather than guessing. Every file it edits is backed up first.

| Flag | Effect |
|---|---|
| `--abi <abi>` | Keep this ABI instead of asking the hub |
| `--strip-source-maps` | Also exclude JavaScript source maps from assets |
| `--store-libs` | Store native libraries uncompressed |
| `--undo` | Restore the gradle files epsh patched |

**Slim APK before every push** in settings applies it automatically. Needs a
Groovy `build.common.gradle`. It refuses to run on a Kotlin DSL project rather
than silently doing nothing.

### Epsh Extreme — reload instead of install (experimental)

Sends only your team code to a running robot and has the FTC SDK reload it,
instead of installing an APK. Measured at **2.89 s instead of about 39 s**
(204 classes, 727 KB).

- **No runtime added to your app.** One marked block goes into
  `TeamCode/build.gradle`: no library, no custom classloader, no bytecode
  rewriting. It uses a reload path the SDK already supports.
- **Same command.** `epsh` decides on each deploy whether to reload or install.
- **It refuses to reload when a reload would leave stale code running.** If
  anything outside team code changed, a library changed, or the robot isn't
  running the build this project produces, it installs instead and says which
  input changed.
- **Files are checked on the robot before the reload is triggered.**
- **It checks that the robot registered your OpModes.** After a reload it asks
  FtcDashboard or Panels for the robot's own OpMode list, compares it with the
  OpModes your source declares, and names any that are missing. If neither
  dashboard answers, it says it couldn't check.
- **FtcDashboard `@Config` keeps working.** Your `@Config` classes are
  registered from inside the reload, including Kotlin `object`s.
- **Hardware drivers stay in the APK.** Device drivers written in team code are
  found and kept in the APK automatically, along with what they need to
  compile, so the rest of the package still reloads.
- **Kotlin is supported**, both `build.gradle.kts` and `.kt` sources. It
  compiles with the Kotlin compiler your project already uses, taken from the
  Gradle cache. Nothing is downloaded.
- **It won't set up alongside Sloth**, because the two break each other on the
  robot. `epsh doctor` flags projects that were set up before this check
  existed.
- **Reloaded code survives restarts and power cycles.**

Set it up in `epsh settings` → **Epsh Extreme**; the same menu undoes it.

Before relying on it:

- Stop the OpMode before reloading.
- Only `org.firstinspires.ftc.teamcode` is reloaded.
- While Epsh Extreme is set up, your team code isn't in the APK. A teammate
  deploying from Android Studio gets a robot with no OpModes.
- It can't be used at the same time as OnBotJava.
- Panels' own class scanning won't see reloaded classes. `epsh dash diff`
  still reads Panels.

### `epsh prepare`

Warms the Gradle cache while you have internet, so builds at a venue don't need
it.

## Connecting to the robot

### `epsh connect` / `epsh exit` / `epsh dc`

- `epsh connect` joins the robot's Wi-Fi and connects ADB. `--find` sweeps the
  current network for the robot instead of only checking where it was last.
- `epsh exit` disconnects ADB and returns you to your usual Wi-Fi.
- `epsh disconnect` (alias `dc`) only drops ADB.

Commands that need the robot offer to connect when nothing is attached. In
menus, `c` does the same. Nothing is asked when epsh isn't running in a
terminal, so scripts behave as before.

### `epsh relay` — put the robot on your Wi-Fi

Plug a USB Wi-Fi adapter into the Control Hub and the hub joins your network
while still serving its own access point to the Driver Station. Deploys,
FtcDashboard, Panels and the Limelight then all work over the network you're
already on.

- `epsh relay setup "<wifi>" "<password>"` joins the network and keeps the
  robot on it.
- `epsh relay forget` undoes it and leaves the hub as it was.
- **The drivers are built in.** Epsh carries RTL8822BU/RTL8812BU drivers (for
  example, the TP-Link Archer T4U v3) built from REV's kernel source, one for
  each Control Hub OS release from 1.0.0 to 1.1.6. It picks the right one by OS
  release, then by kernel release. A hub with no matching driver is told so by
  name.
- **It stays connected.** A boot hook rejoins the network after every power
  cycle and keeps watching: unplug the adapter, plug it back in, and the robot
  is back on the network within about 30 seconds.
- **You browse networks rather than type them.** In `epsh settings` →
  **ADB relay**, the scan runs on the robot itself, showing signal, band and
  open networks as the robot sees them. Networks it has joined before are
  remembered.

### `epsh ip` — where the robot is

Shows the robot's addresses and hostname, then probes every service and lists
only what actually answered: ADB, the Robot Controller page, FtcDashboard,
Panels, the Panels socket and the Limelight. `--adapters` shows which USB
adapters the hub can see, which ones its kernel can drive, and what the kernel
logged about them.

### Per-OS network handling

- **macOS:** reads the saved-network list to work out your current network,
  with no Location Services permission needed.
- **Linux:** uses NetworkManager (`nmcli`).
- **Windows:** uses `netsh` and generates a WPA2-PSK profile to join.

**Home Wi-Fi network** in settings pins the network to return to on any
platform. It always wins.

## Looking at a run

### `epsh power` — what drew the current

Turn on **Power monitor** in settings, deploy, run an OpMode, stop it, then:

- `epsh power` prints the newest run: peak and average current per motor, the
  hub total, and battery sag. Motors are ranked by charge drawn, not by peak.
- **Power readings** in settings lists every recorded run, newest first, and
  opens any of them as an interactive graph. You can drag to zoom, toggle lines
  in the legend, and read every value at a point in time. The battery graph
  follows the same window.
- **Sample rate** can be set from 20 ms to 500 ms.
- `--text` prints instead of opening a page. `--clear` deletes the recordings
  from the robot.

Motors are measured individually. The servo bus and I2C bus get one number
each. A Limelight can't report its current draw, so its load is shown instead.

### `epsh profile` — what ate the loop time

Turn on **Loop profiler** in settings, deploy, run an OpMode, stop it, then
`epsh profile`.

- Draws a **flame chart** of the OpMode's thread. Your own code is in blue, and
  you can click any bar to zoom into it.
- The table ranks methods by time spent *in* them, not under them.
- Nothing has to be instrumented. A sampler thread reads the OpMode thread's
  stack at the **Profile rate** (2–50 ms) and finds the right thread by looking
  for your OpMode class.
- The page says how much of the run the sampler actually saw.
- **Loop profiles** in settings lists past runs. `--text` and `--clear` work as
  they do for `epsh power`.

Both monitors are added as one generated file in an `epshpower` package, which
attaches itself through `@WebHandlerRegistrar`. Your OpModes aren't edited.
Turning the setting off deletes the file. With Epsh Extreme set up, the monitor
is kept in the APK automatically.

## Tuning and configuration

### `epsh dash diff` — tuning you haven't saved

Reads the robot's FtcDashboard or Panels over its WebSocket and compares every
value with the initialiser in your `@Config` / `@Configurable` source.

- **Values that differ** are tuning you haven't written into the code yet.
- **Values that match now but differed last time** are tuning you've already
  saved.
- Works over USB (by forwarding a port) and over Wi-Fi.
- Each run records a reading for the next comparison; `--no-save` skips that.

### `epsh hwconfig` (alias `hw`) — hardware configurations

Brings the robot's configuration XML files into your project's `configs/`
folder so they can be read, edited and committed next to the code. Run it with
no subcommand for the menu.

| Subcommand | What it does |
|---|---|
| `list` (`ls`) | What the robot and the project each have |
| `pull [name...]` | Copy configs off the robot |
| `push [name...]` | Copy configs to the robot (`--force`, `--no-backup`, `--restart`) |
| `view <name>` | Show what is wired where (`--raw` for the file) |
| `edit <name>` | Open in `$EDITOR`, check it, offer to push (`--yes`) |
| `diff [name...]` | Compare the project's copy with the robot's |
| `check [name...]` | Find what the robot would reject |
| `rm <name>` | Delete from the robot (`-y`) |

`--dir` keeps the configs somewhere other than `configs/`.

- **Menu editor:** works in terms of ports and devices rather than XML.
  - Device types autocomplete from the SDK's own list.
  - A new device goes on the lowest free port.
  - Problems show up as you type.
  - Nothing is written until you save.
- **Byte-for-byte.** Files are never rewritten, only parsed to check them, so a
  rename shows up as a one-line diff.
- **Pushes are checked.**
  - Before the push: duplicate names, shared ports, ports the hub doesn't have,
    and an Expansion Hub on the Control Hub's address.
  - After the push: the file is read back off the robot and compared byte for
    byte.
- **Overwrites are backed up** to `configs/.epsh-backup/` first.
- Epsh tells you when you've pushed the active configuration, which needs to be
  re-activated on the Driver Station before it takes effect.

## Settings and maintenance

### `epsh settings` (alias `config`)

An interactive menu. Changes save immediately to `~/.config/epsh/config.yaml`.

- Robot profiles
- Home Wi-Fi network, Return to previous Wi-Fi, Prefer USB when attached
- Slim APK before every push, Send only changed parts, Gradle threads, Deploy
  speed
- Epsh Extreme
- Dashboard tuning check
- Power monitor, Power readings, Sample rate
- Loop profiler, Loop profiles, Profile rate
- ADB relay
- Update epsh, Tell me about updates

### `epsh doctor`

Checks everything epsh depends on and reports it in sections:

- **Wi-Fi:** radio state, IP address, current network, and which OS backend is
  used.
- **Robot Wi-Fi:** whether the profile's network is broadcasting.
- **ADB:** the Android Debug Bridge connection.
- **Project:** FTC project readiness, including a Sloth conflict with Epsh
  Extreme.

### `epsh update`

Installs the latest release. A Homebrew install is handed to `brew upgrade`;
anything else replaces its own binary after checking it against the release
checksums. `--check` reports what's available without installing anything.

Epsh also checks for a new release once a day in the background and shows a
desktop notification (osascript on macOS, notify-send on Linux, a toast on
Windows). Each version is announced once. Turn it off with **Tell me about
updates**, or set `EPSH_NO_NOTIFY=1` to keep the check and drop only the
notification.

### `epsh dev` — developer tools

These deploy to the robot repeatedly, so they're for measuring, not everyday
use.

- **Benchmark the deploy:** times each deploy setting three times over, which
  takes about 15 minutes.
- **Hot reload feasibility:** times pushing a dex to the hub and compiling it
  there, without installing anything.
- **Both, with a full report:** runs the two above and includes Sloth's
  published figures for comparison.
- **Benchmark Epsh Extreme:** reloads your team code several times and times
  each stage.
- **Hot reload an OpMode:** a proof of concept, with a matching **Remove the
  hot reload proof**.
- **Collect the robot's own logs:** pulls the Robot Controller's log files and
  shows the most recent crash. Use it when `adb logcat` comes back empty.

### Other

- `epsh help` lists every command. `epsh --version` (`-v`) shows the version.
- **No telemetry.** The only network call epsh makes on its own is the GitHub
  release check.
- Removed from this fork: the blob library integration, `epsh visualiser`,
  follower logs, and device counting. See [docs/blob.md](docs/blob.md).
