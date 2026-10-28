# neo-angband

This project modernizes Angband 4.2.6. The goal is a headless C engine that a separate Go client, built on the Limoni terminal framework, can play. The plan is in [docs/scenario-2-plan.md](docs/scenario-2-plan.md).

## What is where

| Folder | What it holds |
|---|---|
| `engine/` | The Angband C source. The first commit, tagged `upstream-4.2.6`, is the unmodified release. |
| `client/` | `neoband`, the Go client. It is a stub until Phase 3. |
| `proto/` | The specification for the protocol between the engine and the client. |
| `scripts/` | Build and test scripts. |
| `docs/` | Plans and notes. |

## Build and test

You need CMake, make, a C compiler, the ncurses development headers, Perl, and Go 1.25 or newer. Run this command from the repository root:

```
scripts/check
```

It builds the engine under `build/engine`, runs every engine test, and then builds, vets, and tests the Go client. It stops at the first failure. Add `--clean` to start from an empty build folder.

To play the classic terminal version while the client is being built:

```
build/engine/game/angband -mgcu
```

## Licenses

The engine is under GPL version 2 or the Angband licence; see `engine/docs/copying.rst`. Limoni, which the client uses, is under Apache 2.0. The two must stay in separate programs that talk over a socket. Do not link them into one binary.
