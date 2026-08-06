```
From Team #14270

 ██████╗ ██╗   ██╗ █████╗ ███╗   ██╗████████╗██╗   ██╗███╗   ███╗
██╔═══██╗██║   ██║██╔══██╗████╗  ██║╚══██╔══╝██║   ██║████╗ ████║
██║   ██║██║   ██║███████║██╔██╗ ██║   ██║   ██║   ██║██╔████╔██║
██║▄▄ ██║██║   ██║██╔══██║██║╚██╗██║   ██║   ██║   ██║██║╚██╔╝██║
╚██████╔╝╚██████╔╝██║  ██║██║ ╚████║   ██║   ╚██████╔╝██║ ╚═╝ ██║
 ╚══▀▀═╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═══╝   ╚═╝    ╚═════╝ ╚═╝     ╚═╝

 ██████╗  ██████╗ ██████╗  ██████╗ ████████╗██╗ ██████╗███████╗
██╔══██╗██╔═══██╗██╔══██╗██╔═══██╗╚══██╔══╝██║██╔════╝██╔════╝
██████╔╝██║   ██║██████╔╝██║   ██║   ██║   ██║██║     ███████╗
██╔══██╗██║   ██║██╔══██╗██║   ██║   ██║   ██║██║     ╚════██║
██║  ██║╚██████╔╝██████╔╝╚██████╔╝   ██║   ██║╚██████╗███████║
╚═╝  ╚═╝ ╚═════╝ ╚═════╝  ╚═════╝    ╚═╝   ╚═╝ ╚═════╝╚══════╝
```

# Pusher

One command to build an FTC project and deploy it to the robot.

```bash
pusher
```

If a hub is on USB it uses that and leaves your Wi-Fi alone. Otherwise it builds
first, joins the robot's Wi-Fi, deploys, and puts you back on the network you
started on.

## Install

```bash
brew install PzmuV1517/PzmuV1517/pusher
```

Or from source:

```bash
go build -o pusher && sudo mv pusher /usr/local/bin/
```

Requires `adb` and an FTC project with a Gradle wrapper.

| OS | Wi-Fi switching via | adb |
|---|---|---|
| macOS | `networksetup` | `brew install android-platform-tools` |
| Debian/Ubuntu | `nmcli` (`sudo apt install network-manager`) | `sudo apt install adb` |
| Windows | `netsh` + PowerShell | Android SDK Platform-Tools |

## Commands

| Command | Description |
|---|---|
| `pusher` | Build and deploy |
| `pusher connect` | Join the robot Wi-Fi and connect adb |
| `pusher exit` | Disconnect adb and return to your Wi-Fi |
| `pusher dc` | Disconnect adb only |
| `pusher settings` | Profiles and preferences |
| `pusher slim` | Shrink the APK (`--undo` to revert) |
| `pusher hwconfig` | Pull, edit and push the robot's hardware configs |
| `pusher doctor` | Diagnose Wi-Fi, adb and project problems |
| `pusher dev` | Measure what a deploy costs (see the warning) |
| `pusher visualiser <OpMode>` | Draw the path an auto drove, coloured by speed |
| `pusher prepare` | Cache Gradle dependencies while online |
| `pusher help` | Help |

## Settings

`pusher settings` opens a menu covering robot profiles, which network to return
to, whether to prefer USB, slimming, delta transfer, and Gradle threads. Changes
save immediately to `~/.config/pusher/config.yaml`.

**Update pusher** checks for a newer release and installs it. A Homebrew install
is handed to `brew upgrade` so the next one does not undo it; anything else
replaces its own binary, verified against the release checksums.

## Hardware configurations

The robot's hardware configuration is one XML file in `/sdcard/FIRST` that the
Driver Station writes. `pusher hwconfig` brings those into your project so they
can be read, edited and committed next to the code that names the devices.

Run it on its own and it opens a menu covering all of it. Everything below is
also a subcommand, for scripting or for when you know exactly what you want:

```
pusher hwconfig                 open the menu
pusher hwconfig list            what the robot and the project each have
pusher hwconfig pull            copy the robot's configs into configs/
pusher hwconfig view comp       show what is wired where
pusher hwconfig edit comp       open it in $EDITOR, check it, offer to push
pusher hwconfig diff            what changed against the robot
pusher hwconfig push comp       copy it back
```

Configurations land in `configs/` at your FTC project root. Use `--dir` to keep
them somewhere else.

### The editor

The menu's editor works on ports and devices rather than on XML, which is what
lets it help:

- **Device types autocomplete.** Type `pinpoint` and it finds
  `goBILDAPinpoint`; type `go` and it lists the goBILDA parts. The list is every
  type the SDK ships, read out of the FTC jars.
- **New devices land on a free port.** Pick a type and the port is filled in
  with the lowest one nothing is using — per bus, for I2C.
- **Problems show up as you type**, not after a failed push: a name that is
  already taken, a port that is already used, a port the hub does not have.
- **Nothing is written until you save.** Backing out of an edit leaves the file
  untouched, and the whole tree is marked with what is wrong before you push.

Reading, saving and pushing all preserve the file byte for byte apart from what
you actually changed — same declaration, same indentation, same attribute order
as the Driver Station writes. A rename comes out as a one-line diff.

If you would rather use your own editor, `pusher hwconfig edit <name>` opens
`$EDITOR` on the raw XML and checks it when you save.

Files move byte for byte in both directions — pusher parses them to check and
describe them, never to rewrite them.

**Before pushing**, each file is checked for what the robot controller would
reject: two devices sharing a name, two devices on one port, a port the hub does
not have, an Expansion Hub on the address reserved for the Control Hub. Errors
stop the push (`--force` overrides); anything pusher is unsure about is a
warning. Device types it does not recognise — your own OnBotJava or external
library drivers — still have their names checked but are left alone otherwise.

**Overwriting is guarded.** The robot's copy of anything about to be replaced is
saved into `configs/.pusher-backup/` first, because it may have been changed on
the Driver Station since you pulled it. `--no-backup` skips that.

**Pushing does not activate.** The robot controller reads a configuration when
it is selected, not while it is running one, so overwriting the active file
changes nothing until you re-select it on the Driver Station: Configure Robot →
pick it → Activate. Pusher says so when the file you pushed is the active one.

Reading *which* configuration is active needs privileged adb. That works on a
Control Hub; on a phone robot controller pusher says it could not tell rather
than guessing.

## Visualising an autonomous

`pusher visualiser CloseBlue` pulls a path trace off the robot and renders an HTML
page: the whole flow of the auto, every curve coloured by modelled speed, and a
duration estimate next to the measured time.

```bash
pusher visualiser CloseBlue     # newest trace for that OpMode
pusher visualiser               # newest trace on the robot
pusher visualiser --file t.json # a trace you already have
```

Segments are labelled with the `case` they came from. The blob library captures a
stack trace on each path commit and pusher maps the line number back into your
source, so it works whatever shape the auto is: state machine, inheritance chain,
poses from a constants class. Nothing to annotate.

Colour is modelled speed, not commanded power. Pusher runs a forward/backward
sweep over each curve capped by `maxPower`, by acceleration, and by how hard the
curve bends, so a leg that stays cold is usually cornering-limited and lowering
maxPower there costs you nothing. Tune the model to your drivetrain with
`--top-speed`, `--accel`, `--decel` and `--lat-accel`; the gap between the
estimate and the measured time tells you how far off the defaults are.

Recording requires the `blob-dev` artifact and `BlobParams.recordTrace = true`.
Competition builds of blob contain no recording code at all, so a robot you take
to a match cannot log even if the flag is set.

## Making deploys faster

**Put the Control Hub on 5 GHz.** Hold the hub's button through power-on and
release when the LED turns magenta (yellow is 2.4 GHz). Needs Control Hub OS
1.1.2+. Biggest win available, and it costs nothing.

**Only changed parts are sent.** On by default. The hub keeps the APK in pieces
under `/data/local/tmp/pusher`, which survives reboots, so later pushes transfer
only what differs — measured at 0.6 MB instead of 74 MB for a one-line change.
The rebuilt APK is checksummed on the hub before installing; anything unexpected
falls back to a full transfer.

**`pusher slim`** drops the native libraries for the CPU your hub does not have,
which is about 10 MB of a stock FTC APK. It asks the connected hub which
architecture it runs and refuses to guess, so connect the robot first. Files it
edits are backed up next to themselves; `pusher slim --undo` restores them.

## Deploy speed

A deploy is two halves that behave differently: getting the bytes to the robot,
and the package manager installing them once they arrive. The install is not
just a copy. It writes the APK into `/data/app`, verifies the signature,
extracts the native libraries if they are compressed, and runs dexopt over
every dex file. On a stock FTC project that is tens of megabytes of writes and
tens of megabytes of dex to compile.

`pusher settings` -> **Deploy speed** has a switch for each part of that, because
what wins over USB is not what wins over the robot's 2.4 GHz hotspot:

| setting | what it does | default |
|---|---|---|
| Send only changed parts | sends only the chunks of the APK that changed | on |
| Skip install when unchanged | does nothing at all if the robot already holds this build | on |
| Stream the install | writes the APK straight into an install session instead of pushing it to a temporary file first, halving what gets written on the robot | on |
| Store native libraries uncompressed | stops the install extracting 20 MB+ of libraries, at the cost of a bigger APK. Applied by `pusher slim` | off |
| Install only changed splits | when the project builds a base plus a feature module, installs only the module that changed | off |

The last two are not free. Storing the libraries makes the APK bigger, which
costs transfer time, so it is a win on USB or 5 GHz and a question on 2.4 GHz.
Stored entries also make the delta cache far more effective, because one changed
byte in a deflate stream shifts everything after it while stored bytes do not
move.

Everything falls back safely. A streaming install that the hub does not like
drops to the staged one; a split install with nothing to inherit from installs
the whole APK.

Do not guess which of these to turn on. `pusher dev` measures them.

## Pusher Extreme

Reloads your OpModes onto a running robot instead of installing an APK. Under a
second, against around forty for a normal deploy.

Set it up in `pusher settings` -> **Pusher Extreme**. One marked block is added
to `TeamCode/build.gradle` and nothing else in your project is touched. The same
menu undoes it.

After that a deploy compiles your team code, pushes it to the robot and tells
the robot controller to rescan. Nothing is installed.

**It only reloads when that is equivalent.** If anything outside team code
changed, or the robot is not running the APK this project builds, it installs
normally and says why. Reloading when an install was needed would leave the
robot running stale code with everything reporting success, which is the worst
thing it could do.

**While it is set up, your team code is not in the APK.** That is the point:
parent-first classloading means a class in the APK always wins, so it has to be
absent for a reloaded one to be used. The consequence is that anyone deploying
the project from Android Studio gets a robot with no OpModes until pusher
reloads them. Undo in the menu puts it back, then deploy once.

How it works, if it matters: the FTC SDK already loads classes from outside the
APK for OnBotJava, and already watches a file to know when to rescan. Pusher
compiles your code on your laptop, puts the jar and dex where the SDK reads
them, and touches that file. No `DexClassLoader` of pusher's own, and no changes
to the robot controller app.

### What it does not get on with

**OnBotJava.** They use the same mechanism and the same file to say where
classes live, so whichever ran last wins. Building anything in OnBotJava points
that file at OnBotJava's own output and your reloaded team code stops being
found; the next deploy points it back. Pick one.

**`pusher dev` -> Hot reload an OpMode.** Same thing, on purpose: the proof
replaces whatever is currently loaded, including your team code. Deploy again
afterwards.

**Libraries that go looking for your classes.** Your OpModes can use pedro,
dashboard, ftclib and the rest normally, because those live in the APK and a
reloaded class can see them. The reverse does not hold on its own: a library in
the APK cannot resolve your classes.

Pusher generates a small bridge into the reload, which the SDK runs on every
reload through the same mechanism that finds your OpModes. It sets the thread
context classloader, which is enough for any library that resolves classes that
way, and hands your `@Config` classes to FtcDashboard directly, since dashboard
scans the APK itself and would never find them. Live tuning keeps working.

Checked against pedro, Panels, EasyOpenCV and blob: none of them go looking for
classes on their own, so none of them need anything. A library that does, and
that the bridge does not know about, can have its package kept in the APK
instead.

**`pusher slim --undo`** restores whole gradle files from backups, one of which
holds the Pusher Extreme block. Pusher puts the block back and says so, but if
you edit those files by hand the same trap is there.

**Installing only changed splits** aims at the same cost from the other
direction and does nothing useful once team code is out of the APK.

## pusher dev

Measuring tools for working on pusher itself. **If you do not already know why
you want this, you do not want it** — it deploys to the robot over and over and
reinstalls the app several times.

```
pusher dev
```

- **Benchmark the deploy** times every configuration against the Android Studio
  equivalent, which is one streamed install of the whole APK.
- **Hot reload feasibility** times pushing a team-code-sized dex to the hub and
  compiling it there, to see what a reload would have to beat. Installs nothing.
- **Both, with a full report** writes a report to `pusher-reports/` in your
  project covering the APK's composition, every measured configuration, what
  each setting is worth on your hub, and Sloth's published figures for context.

**Pusher is not a Sloth replacement.** Sloth hot reloads: it sends only the
team's code and loads it into a running app, and reports under a second. Pusher
makes an APK install faster. Those are different problems, and everything pusher
does still ends in a package manager install.

## Per-OS notes

Pusher needs to know which network to put you back on. How it works that out
differs by platform. `pusher doctor` shows which backend is in use.

**macOS** hides the current Wi-Fi name from command-line tools, and your
terminal cannot be added to Location Services by hand — macOS only lists apps
that have already asked, and command-line tools have not been able to ask since
macOS 13. Pusher instead reads the saved-network list, which macOS keeps in
most-recently-joined order, so the network you are on is the first entry. No
permission needed, recomputed every run.

**Debian/Ubuntu** is the easiest case. NetworkManager reports the SSID freely
and records a real last-connected timestamp per saved network, so returning you
to the right network is stored fact rather than inference. Machines managed by
ifupdown or systemd-networkd instead of NetworkManager cannot switch networks;
connect to the robot yourself and pusher will deploy over that connection.

**Windows** reports the SSID freely, so a normal push is fine. But it keeps no
record of when each saved network was last used, so a standalone `pusher exit`
cannot tell where you came from — set the network to return to in
`pusher settings` → Home Wi-Fi network. Note also that `netsh` cannot take a
password inline, so pusher generates a WPA2-PSK profile and imports it before
connecting.

If the network is ever guessed wrong on any platform, pin it in
`pusher settings` → Home Wi-Fi network, which always wins.

## Credits

Made with love by **Andrei "PzmuV1517" Banu**

From **Team #14270**

MIT licensed.
