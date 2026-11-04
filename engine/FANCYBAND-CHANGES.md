# Fancyband's changes to Angband

This folder began as the unmodified source of Angband 4.2.6, in the commit tagged `upstream-4.2.6`. Fancyband has changed it since 2026-10-28.

## How to find each change and its date

- **In a clone of the repository,** run `git log --date=short --stat upstream-4.2.6..HEAD -- engine`. To see one file's changes, name the file instead of `engine`.
- **In a source release,** read `FANCYBAND-CHANGE-LOG.txt` in this folder, which lists the same commits.
- **In a changed C source file or header,** the comment at the top says the file changed and gives the date of its first change. `scripts/change-notices` adds that comment, and `scripts/check` fails if one is missing.

Other changed files, such as build files and data files, have no such comment; the log lists them.

## What changed

- **The name.** The program is `fancyband`, and it keeps its files in `~/.angband/Fancyband`.
- **A new front end.** `main-net.c`, `net-*.c`, and `net-*.h` stream the game to a separate client program over a socket, and answer its questions about the game.
- **Screens the client can draw.** The text interface gained hooks in `ui-*.c` that hand a screen or prompt to the client, and functions that give the client the same information the text screens show.
- **Separating the engine from the text interface.** Game code no longer depends on text interface headers. For example, the spoiler commands moved to `ui-cmd-spoil.c`.
- **New data.** `lib/tiles/fancyband/` holds Fancyband's default tiles, and `lib/help/descriptions.txt` holds text taken from the game's manual.
- **Tests and build files.** The test runners now report failures, and the build files know the new source files.

Gameplay rules are meant to stay as they are in Angband 4.2.6.
