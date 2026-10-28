# Engine protocol, version 1

This is the protocol between the `fancyband` engine and its clients, such as `fancyband-tui` and the `netdump` debugging tool. The engine streams its screen as character grids, and the client sends keys back. Phase 4 of [the plan](../docs/scenario-2-plan.md) adds structured messages to the same connection.

Two programs implement this file:

- The engine side is `engine/src/net-proto.c` (framing) and `engine/src/main-net.c` (the `net` front end).
- The client side is the Go package `client/internal/proto`.

Both test against the vectors in [vectors.txt](vectors.txt). If you change the protocol, change this file, both programs, and the vectors together.

## Contents

- [Starting the engine](#starting-the-engine)
- [Frames](#frames)
- [The handshake](#the-handshake)
- [Server-to-client messages](#server-to-client-messages)
- [Client-to-server messages](#client-to-server-messages)
- [Attributes and colors](#attributes-and-colors)
- [Key codes](#key-codes)
- [How the session ends](#how-the-session-ends)
- [Debugging with netdump](#debugging-with-netdump)

## Starting the engine

The engine takes its own options first, then `--`, then the options of the `net` module. The module accepts the connection one of two ways:

| Command | Use |
|---|---|
| `fancyband -mnet [options] -- --fd N` | Use socket `N`, inherited from the parent. Clients use this. They create a socket pair and pass one end. |
| `fancyband -mnet [options] -- --listen PATH` | Wait on a Unix socket at `PATH` for one client, then stop listening and remove the socket. Use this for debugging. |

The engine needs a UTF-8 locale. With the default build it finds its `lib/` folder relative to the working directory, so start it in the folder that holds the binary.

To keep a test game away from your real files, point every folder the game writes to at a scratch folder. Put the `-d` options before `-u`, because `-u` reads the save folder when the engine parses it:

```
fancyband -mnet -duser=S/user -darchive=S/user/archive -dsave=S/save \
    -dpanic=S/panic -dscores=S/scores -utester -n -- --fd 3
```

## Frames

Every message is one frame. All numbers are little-endian.

```
uint32  length   counts the type byte and the payload, so it is at least 1
uint8   type
bytes   payload
```

A frame may be at most 1 MiB long, counting the type byte. A length of 0 or over the limit means the stream is corrupt; the engine then exits as if the client had disconnected.

Each side ignores message types it does not know, so either side can add messages without breaking the other.

In the tables below, `u8`, `u16`, and `u32` are unsigned integers of 8, 16, and 32 bits. `str8` is a `u8` byte count followed by that many bytes of UTF-8. `rest` is UTF-8 text that fills the rest of the payload.

## The handshake

1. The client sends `HELLO`. It lists the terms it wants and the capabilities it claims.
2. The engine answers with its own `HELLO`.
3. The engine sends `TERM_OPEN` for each term it opens, then `PALETTE`.
4. The game starts. The first screen is the splash screen, which waits for any key.

`HELLO` has the same layout in both directions:

| Field | Type | Meaning |
|---|---|---|
| version | `u16` | Protocol version. This document describes version 1. |
| name | `str8` | The program's name. The engine sends its build name, for example `Fancyband 4.2.6`. |
| term count | `u8` | Number of term entries that follow. The engine sends 0. |
| terms | 5 bytes each | `u8` term index (0 to 7), `u16` width, `u16` height |
| capability count | `u8` | Number of capabilities that follow |
| capabilities | `str8` each | Capability names. None are defined yet; Phase 4 adds `events`. |

The engine always opens term 0, the main term, even if the client does not ask for it. The main term is at least 80 by 24; the engine enlarges a smaller request. Other terms are at least 1 by 1. No term is larger than 1000 in either direction.

If the client's version is not 1, the engine sends its `HELLO`, then `BYE` with a reason that names the version, and exits with a failure status.

## Server-to-client messages

| Type | Name | Payload | Meaning |
|---|---|---|---|
| `0x01` | `HELLO` | See [the handshake](#the-handshake) | The engine's answer to the client's `HELLO` |
| `0x10` | `TERM_OPEN` | `u8` term, `u16` width, `u16` height, `rest` name | A term exists at this size. The engine also sends it after a `RESIZE`, with the size the term actually got. The client blanks the term. |
| `0x11` | `PALETTE` | `u8` count, then count × 3 bytes of red, green, blue | The color table. The engine sends 32 colors at start and again when the player changes colors. |
| `0x12` | `TEXT` | `u8` term, `u16` x, `u16` y, `u16` attr, `rest` text | Draw text from (x, y) to the right in one attribute. Each character fills one cell, even a wide one. |
| `0x13` | `WIPE` | `u8` term, `u16` x, `u16` y, `u16` count | Blank `count` cells from (x, y) to the right |
| `0x14` | `CLEAR` | `u8` term | Blank the whole term |
| `0x15` | `CURSOR` | `u8` term, `u16` x, `u16` y, `u8` flags | Where the cursor is. Flag `0x01` means visible; `0x02` means it covers a double-width tile. |
| `0x16` | `FRESH` | `u8` term | The end of a frame for this term. The client shows changes only when it gets `FRESH`. |
| `0x17` | `BELL` | none | Signal the player, for example with a flash |
| `0x18` | `SOUND` | reserved | Not sent yet |
| `0x20` | `EVENT` | reserved | Phase 4 |
| `0x21` | `SNAPSHOT` | reserved | Phase 4 |
| `0x22` | `REQUEST` | reserved | Phase 4 |
| `0x3F` | `BYE` | `rest` reason | The engine's last message before it exits |

The engine sends `CURSOR` just before `FRESH`, and only when the cursor changed. The client applies `TEXT`, `WIPE`, `CLEAR`, and `CURSOR` to a hidden copy of the term and shows that copy on `FRESH`. The Go package `client/internal/grid` does this.

The engine sends text that is not valid Unicode, such as a lone surrogate, as U+FFFD.

## Client-to-server messages

| Type | Name | Payload | Meaning |
|---|---|---|---|
| `0x01` | `HELLO` | See [the handshake](#the-handshake) | Must be the first frame. The engine ignores later ones. |
| `0x40` | `KEY` | `u32` key code, `u8` modifiers | One key press. See [key codes](#key-codes). |
| `0x41` | `MOUSE` | `u8` term, `u16` x, `u16` y, `u8` button, `u8` modifiers | One click. Buttons are 1 to 4. The engine uses clicks on term 0 only. |
| `0x42` | `RESIZE` | `u8` term, `u16` width, `u16` height | Resize an open term. The engine answers with `TERM_OPEN` and redraws. |
| `0x50` | `COMMAND` | reserved | Phase 4 |
| `0x51` | `REPLY` | reserved | Phase 4 |
| `0x7F` | `QUIT` | none | Save and exit. See [how the session ends](#how-the-session-ends). |

The engine reads from the socket only when the game waits for input. While it waits, it wakes every 200 ms so the game can animate.

## Attributes and colors

An attribute packs three things into 16 bits:

| Bits | Meaning |
|---|---|
| 0 to 6 (`attr & 0x7F`) | Color: an index into the palette |
| 7 (`attr & 0x80`) | Reverse video: swap foreground and background |
| 8 to 15 (`attr >> 8`) | Background mode: 0 is black, 1 is the same as the foreground color (used for solid walls), 2 is dark |

## Key codes

A key code is a Unicode code point for a printable key. Special keys use the codes from `engine/src/ui-event.h`:

| Key | Code | Key | Code |
|---|---|---|---|
| Arrow down | `0x80` | Home | `0x94` |
| Arrow left | `0x81` | Page up | `0x95` |
| Arrow right | `0x82` | End | `0x96` |
| Arrow up | `0x83` | Page down | `0x97` |
| F1 to F15 | `0x84` to `0x92` | Insert | `0x98` |
| Enter | `0x9C` | Delete | `0x9E` |
| Tab | `0x9D` | Backspace | `0x9F` |
| Escape | `0xE000` | | |

A control letter is the letter's code with only the low five bits kept, as with `KTRL()` in `ui-event.h`. For example, Ctrl+X, the game's save-and-quit key, is `0x18`.

The modifier bits are control `0x01`, shift `0x02`, alt `0x04`, meta `0x08`, and keypad `0x10`. Send number-pad keys as the digit with the keypad bit set, for example `'8'` with `0x10`, so the game can tell them from the number row.

## How the session ends

The engine sends `BYE` before it exits. The reason tells the client why:

| Reason | What happened |
|---|---|
| `exit` | The game ended normally, for example after Ctrl+X |
| `quit` | The client sent `QUIT` |
| An error message | The game stopped on an error |

When the client sends `QUIT` or the connection drops, the engine saves, then exits. What it saves depends on the game's state:

- **Playing:** it saves the game the same way Ctrl+S does, then exits.
- **Creating a character:** it exits without saving.
- **Dead:** it saves the character as dead, so that dropping the connection on the death screen cannot bring the character back.

While it saves, the engine answers any prompt the game shows with Escape. It exits with status 0 in all three cases.

## Debugging with netdump

`netdump` prints frames as text. Build it with `go build ./cmd/netdump` in `client/`.

```
netdump FILE                     print the frames in a captured stream; "-" reads stdin
netdump -connect PATH            talk to an engine started with -- --listen PATH
netdump -run BINARY [ARGS...]    start BINARY -mnet ARGS... and talk to it
```

Add `-screen` to print the main term as text after each `FRESH`, instead of one line per frame. When `netdump` talks to an engine, each line you type is sent as key presses. Plain characters go as they are, and these tokens send other keys: `<esc>`, `<enter>`, `<tab>`, `<bs>`, `<up>`, `<down>`, `<left>`, `<right>`, `<^X>` for any control letter, `<kp8>` for a number-pad digit, and `<quit>` for `QUIT`.
