# Fancyband

Fancyband is a variant of [Angband](http://rephial.org/) 4.2.6. It modernizes the game: the goal is a headless C engine that a separate Go client, built on the Limoni terminal framework, can play. The plan is in [docs/scenario-2-plan.md](docs/scenario-2-plan.md).

## What is where

| Folder | What it holds |
|---|---|
| `engine/` | The game's C source, which builds the `fancyband` program. The first commit, tagged `upstream-4.2.6`, is the unmodified Angband release. |
| `client/` | The Go client, `fancyband-tui`, built on Limoni. `netdump` prints what the engine sends, for debugging. |
| `proto/` | The protocol between the engine and the client, and test vectors that both sides check. |
| `scripts/` | Build and test scripts. |
| `docs/` | Plans and notes. |

## Build and test

You need CMake, make, a C compiler, the ncurses development headers, Perl, and Go 1.25 or newer. Run this command from the repository root:

```
scripts/check
```

It checks that engine code includes no UI headers, builds the engine under `build/engine`, runs every engine test, and then builds, vets, and tests the Go client. The client's tests include end-to-end tests that play the engine over its `net` front end. It stops at the first failure. Add `--clean` to start from an empty build folder. You also need Python 3 for the header check.

## Play

After `scripts/check` has built the engine, start the client from the repository:

```
cd client
go run ./cmd/fancyband-tui -- -n
```

Options after `--` go to the engine: `-n` starts a new character, and `-uName` picks a saved one. The client finds the engine in `build/engine/game`; pass `-engine PATH` to use another one. Press Ctrl+X, the game's own key, or Ctrl+C to save and quit.

The window must be at least 80 columns by 24 rows. When it is wider than about 105 columns, a side panel shows the message log and the monster list; when it is taller than about 31 rows instead, a bottom panel shows the message log. `-subwindows=false` turns the panels off.

To draw the map with tiles, add `-tiles 2` for Adam Bolt's set; 1, 3, and 4 pick the others. Tiles need a terminal that can show pictures, through the Kitty or Sixel image protocol. Without one, or inside tmux, the client logs why and draws text. `-graphics kitty` or `-graphics sixel` picks the protocol when the client guesses wrong. The client picks the Kitty protocol in Kitty and Sixel in Konsole, which suits each best. Tiles send more than text: about 3 KB a move with the Kitty protocol and 90 KB with Sixel, against a few hundred bytes for text.

Lava ripples between red and yellow, in text and in tiles. `-animate=false` turns this off. With Sixel, each lava tile on screen is sent again at every step of the ripple, so a big pool costs more over a slow link.

The client writes `fancyband-tui.log`, and the engine's error output `fancyband-engine.log`, to `~/.angband/Fancyband`. `-log-dir` picks another folder.

### Keep test games apart

Fancyband keeps your monster memory, preferences, and character summary in `~/.angband/Fancyband`. A normal Angband install uses `~/.angband/Angband`, so the two never share that data. Saved games go to `build/engine/game/lib/save`. To keep a test game out of both, point the engine at scratch folders. Put the `-d` options before `-u`:

```
mkdir -p /tmp/fbtest/user/archive /tmp/fbtest/save
go run ./cmd/fancyband-tui -log-dir /tmp/fbtest -- \
    -duser=/tmp/fbtest/user -darchive=/tmp/fbtest/user/archive -dsave=/tmp/fbtest/save -n
```

### Other ways to run the engine

The classic curses version still works:

```
build/engine/game/fancyband -mgcu
```

To see the raw stream the client gets, run the engine through `netdump`:

```
cd client && go build -o /tmp/netdump ./cmd/netdump
/tmp/netdump -screen -run ../build/engine/game/fancyband -n
```

Type keys and press Enter to send them; `<quit>` saves and exits. [proto/README.md](proto/README.md) explains the protocol and the other `netdump` options.

## Make a release

```
scripts/dist --portable
```

This writes three files to `build/dist`: a tarball with both programs, the game's `lib/` folder, a README, and the licenses; a source tarball; and their SHA-256 sums. The tarball's README is [docs/playing.md](docs/playing.md), which covers getting started, tiles, and keys for players.

`--portable` builds the engine in an Ubuntu 22.04 container with Docker, so it runs on glibc 2.34 and later. Without it the engine needs the glibc of the machine that built it: a build on a current distribution needs glibc 2.38, which leaves out Ubuntu 22.04 and Debian 12. The client is a static Go program and runs anywhere. The release saves characters in `~/.angband/Fancyband/save`, not in `lib/`, so a newer release keeps them.

Linux is the first target. The script should also work on a Mac, without `--portable`; nobody has tried it there yet.

## Licenses

Fancyband keeps Angband's licenses. The engine is under GPL version 2 or the Angband licence; see `engine/docs/copying.rst`. Limoni, which the client uses, is under Apache 2.0. The client builds a fork of it in `client/third_party/limoni`, which keeps Limoni's license; its `FORK.md` lists the changes. The two must stay in separate programs that talk over a socket. Do not link them into one binary.

The tile sets have their own terms, also in `copying.rst`. Adam Bolt's may be used for any purpose, and David Gervais' under Creative Commons Attribution 3.0. Shockbolt's licence allows its tiles with Angband and its variants, free of charge, and forbids changing them. Releases ship the set unchanged; the client's lava ripple recolors tiles on screen as it draws them and never alters the sheet.
