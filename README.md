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

Requires macOS, `adb` (`brew install android-platform-tools`), and a `gradlew`
in your FTC project.

## Commands

| Command | Description |
|---|---|
| `pusher` | Build and deploy |
| `pusher connect` | Join the robot Wi-Fi and connect adb |
| `pusher exit` | Disconnect adb and return to your Wi-Fi |
| `pusher dc` | Disconnect adb only |
| `pusher settings` | Profiles and preferences |
| `pusher slim` | Shrink the APK (`--undo` to revert) |
| `pusher doctor` | Diagnose Wi-Fi, adb and project problems |
| `pusher prepare` | Cache Gradle dependencies while online |
| `pusher help` | Help |

## Settings

`pusher settings` opens a menu covering robot profiles, which network to return
to, whether to prefer USB, slimming, delta transfer, and Gradle threads. Changes
save immediately to `~/.config/pusher/config.yaml`.

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

## macOS and Wi-Fi names

macOS hides the current Wi-Fi name from command-line tools, and your terminal
cannot be added to Location Services by hand — macOS only lists apps that have
already asked, and command-line tools have not been able to ask since macOS 13.

Pusher works around it by reading the saved-network list, which macOS keeps in
most-recently-joined order, so the network you are on is the first entry. That
needs no permission and is recomputed every run, so moving between home, the lab
and a competition venue needs no setup. If it ever guesses wrong, pin it in
`pusher settings` → Home Wi-Fi network.

## Credits

Made with love by **Andrei "PzmuV1517" Banu**

From **Team #14270**

MIT licensed.
