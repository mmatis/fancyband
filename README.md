# Fancyband

Fancyband is a variant of [Angband](http://rephial.org/) 4.2.6. It modernizes the game: the goal is a headless C engine that a separate Go client, built on the Limoni terminal framework, can play. The plan is in [docs/scenario-2-plan.md](docs/scenario-2-plan.md).

## What is where

| Folder | What it holds |
|---|---|
| `engine/` | The game's C source, which builds the `fancyband` program. The first commit, tagged `upstream-4.2.6`, is the unmodified Angband release. |
| `client/` | The Go client. `fancyband-tui` is a stub until Phase 3. `netdump` prints what the engine sends, for debugging. |
| `proto/` | The protocol between the engine and the client, and test vectors that both sides check. |
| `scripts/` | Build and test scripts. |
| `docs/` | Plans and notes. |

## Build and test

You need CMake, make, a C compiler, the ncurses development headers, Perl, and Go 1.25 or newer. Run this command from the repository root:

```
scripts/check
```

It checks that engine code includes no UI headers, builds the engine under `build/engine`, runs every engine test, and then builds, vets, and tests the Go client. The client's tests include end-to-end tests that play the engine over its `net` front end. It stops at the first failure. Add `--clean` to start from an empty build folder. You also need Python 3 for the header check.

To play the classic terminal version while the client is being built:

```
build/engine/game/fancyband -mgcu
```

Fancyband keeps your monster memory, preferences, and character summary in `~/.angband/Fancyband`. A normal Angband install uses `~/.angband/Angband`, so the two never share that data. To keep a test game out of your real Fancyband folder, point it at another one:

```
mkdir -p /tmp/fancyband-test/archive
build/engine/game/fancyband -mgcu -duser=/tmp/fancyband-test -darchive=/tmp/fancyband-test/archive
```

Saved games go to `build/engine/game/lib/save` either way.

To watch the engine stream its screen over a socket, the way the Go client will use it, run it through `netdump`:

```
cd client && go build -o /tmp/netdump ./cmd/netdump
/tmp/netdump -screen -run ../build/engine/game/fancyband -n
```

Type keys and press Enter to send them; `<quit>` saves and exits. [proto/README.md](proto/README.md) explains the protocol and the other `netdump` options.

## Licenses

Fancyband keeps Angband's licenses. The engine is under GPL version 2 or the Angband licence; see `engine/docs/copying.rst`. Limoni, which the client uses, is under Apache 2.0. The two must stay in separate programs that talk over a socket. Do not link them into one binary.
