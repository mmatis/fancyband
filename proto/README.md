# Engine protocol, version 1

This is the protocol between the `fancyband` engine and its clients, such as `fancyband-tui` and the `netdump` debugging tool. The engine streams its screen as character grids, and the client sends keys back. On the same connection, a client can also get game events and snapshots of what the player knows, send game commands, and answer the game's prompts. In headless mode the engine runs with no screen at all, and those structured messages are the whole interface.

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
- [The structured channel](#the-structured-channel)
- [Headless mode](#headless-mode)
- [How the session ends](#how-the-session-ends)
- [Debugging with netdump](#debugging-with-netdump)

## Starting the engine

The engine takes its own options first, then `--`, then the options of the `net` module. The module accepts the connection one of two ways:

| Command | Use |
|---|---|
| `fancyband -mnet [options] -- --fd N` | Use socket `N`, inherited from the parent. Clients use this. They create a socket pair and pass one end. |
| `fancyband -mnet [options] -- --listen PATH` | Wait on a Unix socket at `PATH` for one client, then stop listening and remove the socket. Use this for debugging. |

Add `--headless` after either one to run with no text UI and no screen. See [headless mode](#headless-mode).

Add `--new-save` with the engine's `-n` option to leave the savefile unnamed until the character is made. The name chosen at birth then names the savefile, and the birth screens ask before overwriting another character's save. Without it, `-n` makes the new character in the savefile `-u` or your login names, replacing what was there.

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
| capabilities | `str8` each | Capability names. See [capabilities](#capabilities). |

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
| `0x19` | `TILESET` | `u8` set number, `u16` tile width and `u16` tile height in pixels, `u8` columns and `u8` rows each tile covers, `u8` the set's alpha-blending flag from `list.txt` (clients blend every set anyway), `str8` name, `rest` the sheet's absolute path | The map is drawn with tiles, or with text again when the set number is 0. See [tiles](#tiles). |
| `0x1A` | `PICT` | `u8` term, `u16` x, `u16` y, `u16` count, then count × 4 bytes: tile row, tile column, terrain row, terrain column | Draw tiles in place of text, one cell after another from (x, y). See [tiles](#tiles). |
| `0x20` | `EVENT` | JSON | Something happened in the game, or the result of a `COMMAND`. See [events](#events). |
| `0x21` | `SNAPSHOT` | JSON | What the player knows. See [snapshots](#snapshots). |
| `0x22` | `REQUEST` | JSON | A prompt the client must answer with `REPLY`. See [prompts](#prompts). |
| `0x3F` | `BYE` | `rest` reason | The engine's last message before it exits |

The engine sends `CURSOR` just before `FRESH`, and only when the cursor changed. It sends `FRESH` only when something changed since the last one. It writes to the socket when the game waits for a key, pauses for an animation, or has 64 KB or more waiting, not at each `FRESH`; the game refreshes after every map grid it redraws, so one move holds 20 to 40 frames, and they arrive together. A client may draw only the last frame of what it has read. The client applies `TEXT`, `WIPE`, `CLEAR`, and `CURSOR` to a hidden copy of the term and shows that copy on `FRESH`. The Go package `client/internal/grid` does this.

The engine sends text that is not valid Unicode, such as a lone surrogate, as U+FFFD.

## Client-to-server messages

| Type | Name | Payload | Meaning |
|---|---|---|---|
| `0x01` | `HELLO` | See [the handshake](#the-handshake) | Must be the first frame. The engine ignores later ones. |
| `0x40` | `KEY` | `u32` key code, `u8` modifiers | One key press. See [key codes](#key-codes). |
| `0x41` | `MOUSE` | `u8` term, `u16` x, `u16` y, `u8` button, `u8` modifiers | One click. Buttons are 1 to 4. The engine uses clicks on term 0 only. |
| `0x42` | `RESIZE` | `u8` term, `u16` width, `u16` height | Resize an open term. The engine answers with `TERM_OPEN` and redraws. |
| `0x50` | `COMMAND` | JSON | Run a game command. See [commands](#commands). |
| `0x51` | `REPLY` | JSON | The answer to a `REQUEST`. See [prompts](#prompts). |
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

## The structured channel

`EVENT`, `SNAPSHOT`, `REQUEST`, `COMMAND`, and `REPLY` frames carry one JSON object, encoded as UTF-8. Each side ignores fields it does not know, so either side can add fields.

**License note.** These messages carry game data, not just a screen. The plan asks for a legal opinion before you distribute builds that use them; see the risks section of [the plan](../docs/scenario-2-plan.md).

### Capabilities

The client asks for structured messages by listing capabilities in its `HELLO`:

| Capability | What the engine does |
|---|---|
| `events` | Sends an `EVENT` for each game event in [the list below](#events). |
| `snapshots` | Sends a `SNAPSHOT` each time the game waits for a command. |
| `snapshots:PARTS` | The same, with only the named [parts](#snapshots), joined with `+`, such as `snapshots:player+gear`. |
| `prompt:KIND` | Sends prompts of that kind as `REQUEST` frames instead of drawing them. `KIND` is one of the names in [the prompts table](#prompts). |
| `prompts` | Claims every kind of prompt. |
| `screen:NAME` | Hands that screen to the client instead of drawing it. See [screens](#screens). |
| `screens` | Claims every screen. |
| `waits` | Sends `WAIT` in screen mode too. See [events](#events). |
| `status` | Sends a `SNAPSHOT` with only the [status part](#the-status-part) each time it changes, just before the screen it belongs to. |
| `moves` | Sends a `MOVES` event with the steps creatures took since the last screen, just before that screen, so the client can slide them. See [events](#events). |
| `scrolls` | Sends a `SCROLL` event just before the game redraws a map that moved, so the client can scroll from the old screen to the new one. See [events](#events). |
| `tiles:N` | Draws the map with tile set `N`, its number in `lib/tiles/list.txt`. See [tiles](#tiles). |

Without a capability the engine behaves as in protocol version 1 without it. Command results and `SNAPSHOT` replies to the `SNAPSHOT` command are always sent.

### Events

Every event has an `event` field with its name. Most carry nothing else, because the client can ask for a snapshot. These carry fields:

| Event | Fields |
|---|---|
| `HP` | `hp`, `mhp` (current and maximum hit points) |
| `MANA` | `sp`, `msp` |
| `AC` | `ac` |
| `EXPERIENCE`, `PLAYERLEVEL` | `level`, `exp`, `max_exp` |
| `GOLD` | `gold` |
| `DUNGEONLEVEL` | `depth` (0 is the town) |
| `PLAYERSPEED` | `speed` (0 is normal speed) |
| `PLAYERMOVED` | `x`, `y` |
| `MESSAGE` | `text`, `type` (a message type from `list-message.h`) |
| `SOUND` | `type` |
| `STATS` | `stats`: an object with `str`, `int`, `wis`, `dex`, `con` |

The other events the engine forwards are `PLAYERTITLE`, `MONSTERHEALTH`, `RACE_CLASS`, `STUDYSTATUS`, `STATUS`, `DETECTIONSTATUS`, `FEELING`, `LIGHT`, `STATE`, `SEEFLOOR`, `EXPLOSION`, `BOLT`, `MISSILE`, `INVENTORY`, `EQUIPMENT`, `ITEMLIST`, `MONSTERLIST`, `MONSTERTARGET`, `OBJECTTARGET`, `BELL`, `STORECHANGED`, `NEW_LEVEL`, `CHEAT_DEATH`, and the context changes `ENTER_BIRTH`, `LEAVE_BIRTH`, `ENTER_GAME`, `LEAVE_GAME`, `ENTER_WORLD`, `LEAVE_WORLD`, `ENTER_STORE`, `LEAVE_STORE`, `ENTER_DEATH`, and `LEAVE_DEATH`.

Two events are not game events. The engine sends them whatever the client claimed:

- `COMMAND` answers each `COMMAND` frame: `ok` is true or false, `error` says why a command was refused, and `id` repeats the command's `id` if it had one.
- `WAIT` says the game waits for a command. `context` is `birth`, `game`, or `store`. Headless mode always sends it. In screen mode the client must claim `waits`; then `WAIT` comes each time the game waits for a command with no menu, prompt, or `-more-` showing, and once with context `splash` while the splash screen waits for a key before a character loads. No `WAIT` comes while the borg plays.
- `MOVES`, with the `moves` capability, lists the creatures that stepped from grid to grid since the engine last sent the screen, each as `path`: the cells on the main term where its tile started and then where it stood after each step, as `[column, row]` pairs. Only steps to a neighboring grid count, and only for the player and for monsters the player sees at the end of the path, with every grid of the path on the map. A creature that jumps, as by teleporting, is left out. `busy` is true while the player runs, rests, or repeats a command, or the borg plays, when the moves come too fast to show.
- `SCROLL`, with the `scrolls` capability, says the map is about to move on the main term, as when the character nears its edge or uses the locate command. It comes before the engine redraws the map, so the client can keep the screen it has. `shift` is how far the map's contents move, in cells, as `[columns, rows]`: a grid at x, y on the old screen is at x plus columns, y plus rows on the new one. `map` is the cells that show the map, as `x`, `y`, `w`, and `h`. `busy` means the same as in `MOVES`. No `SCROLL` comes for a new level, a resize, or a new tile size, since the screen before shows something else.
- `BORG` says the borg started or stopped, whether by the `BORG` command, by a key, or by itself: `on` is true or false.

### Snapshots

A snapshot describes what the player knows, never more:

```json
{
  "turn": 1234,
  "context": "game",
  "player": {"name": "Tester", "race": "Human", "class": "Warrior",
             "level": 1, "exp": 0, "max_exp": 0, "hp": 20, "mhp": 20,
             "sp": 0, "msp": 0, "gold": 134, "depth": 0, "max_depth": 0,
             "x": 40, "y": 5, "speed": 0, "ac": 10, "food": 14999,
             "dead": false,
             "stats": {"str": 18, "int": 8, "wis": 8, "dex": 18, "con": 18}},
  "monsters": [{"name": "Grip, Farmer Maggot's Dog", "x": 45, "y": 9,
                "health": 100, "asleep": true}],
  "inventory": [{"ref": {"inven": 0}, "name": "a Flask of Whisky",
                 "number": 1, "weight": 10}],
  "equipment": [], "quiver": [], "floor": [],
  "map": {"width": 66, "height": 22, "rows": ["#####...", "..."]}
}
```

- `context` is `init`, `birth`, `game`, `store`, or `death`.
- During character creation, `birth` lists the `races` and `classes` by index, for the `CHOOSE_RACE` and `CHOOSE_CLASS` commands. `player` is there once a character exists; `monsters`, the gear lists, and `map` once there is a level.
- A snapshot has six parts that a client can ask for one by one: `player`; `monsters`; `gear`, which is `inventory`, `equipment`, and `quiver`; `floor`; `map`; and `status`. The turn, the context, and the birth choices are always there.
- `monsters` lists only monsters the player can see.
- Weights are in tenths of a pound.
- `map` comes from the player's memory of the level, so it shows only what the player has seen. Each row is a string with one character per grid:

| Character | Grid |
|---|---|
| space | Not known |
| `.` | Open floor |
| `#` | Wall, or rubble that blocks |
| `:` | Rubble you can walk through |
| `+` | Closed door |
| `'` | Open or broken door |
| `<` `>` | Up and down staircase |
| `s` | Shop entrance |
| `^` | Known trap |

#### The status part

`status` holds what the game's sidebar and status line show, once there is a level. Colors are palette indexes.

| Field | What it holds |
|---|---|
| `name`, `race`, `class`, `title` | Who the character is. `race` is empty while the character is shapechanged, and `title` then names the shape. |
| `level`, `max_level` | The level now and the highest reached. |
| `exp`, `max_exp`, `next_exp` | Experience, its highest, and the experience still needed for the next level, or -1 at level 50. |
| `gold` | Gold. |
| `equippy` | One entry per equipment slot: `char` and `color` for what is in it, or `null` for an empty slot. With tiles, and in headless mode, these are the object's own letter and color. With tiles, `tile` also gives the object's tile, as `row` and `col`, numbered as in `PICT`. |
| `stats` | Five entries, `STR` to `CON`: `name`, `value` (as `stat_use`, so 18/20 is 38), `drained` (below its maximum), and `maxed` (its maximum is 18/100). |
| `ac` | Armor class, as the player knows it. |
| `hp`, `mhp`, `hp_color` | Hit points, their maximum, and the color the game gives the number. |
| `magic`, `sp`, `msp`, `sp_color` | Whether the game shows spell points, and the same three for them. |
| `health` | The tracked monster's health: `color` and `percent`, which is -1 when the player cannot tell. `null` when no monster is tracked. |
| `speed`, `speed_text`, `speed_color` | Speed (0 is normal), the game's text for it, such as `Fast (+10)`, empty at normal speed, and its color. |
| `depth`, `depth_text` | Depth, and the game's text for it, such as `Town` or `250' (L5)`. |
| `line` | The status line: a list of spans (`text` and `color`), one per item, in the game's order, such as `Light 1`, `Poisoned`, and `Fed 89 %`. A client puts a space between items. |

#### Item references

Objects are named by where they are. A reference is an object with one field:

| Reference | Object |
|---|---|
| `{"inven": n}` | Pack slot `n`, from 0 |
| `{"equip": n}` | Equipment slot `n` |
| `{"quiver": n}` | Quiver slot `n` |
| `{"floor": n}` | The `n`th object under the player, in snapshot order |
| `{"store": n}` | The `n`th item for sale, in a store |

### Commands

A command names an engine command code, without its `CMD_` prefix, and gives its arguments:

```json
{"id": 7, "cmd": "WALK", "args": {"direction": 6}}
```

- `id` is optional. The `COMMAND` event that answers repeats it.
- `repeat` is optional. It repeats the command that many times, as the game's own count does.
- The argument names are the ones the engine reads, such as `direction`, `item`, `choice`, `quantity`, `point`, `target`, `name`, and `spell`. The value's shape picks the type: an object with `x` and `y` is a point, any other object is an [item reference](#item-references), a string is a string, `direction` and `target` take a number, the names `quantity`, `index`, `tval`, `range`, `level`, `power`, and `depth` take a number, and any other number is a choice.
- If a command lacks an argument it needs, the game asks for it with a prompt, as it would ask a player. Claim that kind of prompt to answer it with a `REPLY`.

The engine checks each command against the current context and refuses it if it does not fit:

| Context | Commands |
|---|---|
| `birth` | `BIRTH_INIT`, `BIRTH_RESET`, `CHOOSE_RACE`, `CHOOSE_CLASS`, `BUY_STAT`, `SELL_STAT`, `RESET_STATS`, `REFRESH_STATS`, `ROLL_STATS`, `PREV_STATS`, `NAME_CHOICE`, `HISTORY_CHOICE`, `ACCEPT_CHARACTER` |
| `game` | `GO_UP` through `SLEEP` in `cmd-core.h`, plus `RETIRE`, `REPEAT`, and `COMMAND_MONSTER` |
| `store` | `SELL`, `BUY`, `STASH`, `RETRIEVE` |
| none | The splash-screen, help, spoiler, and debug commands |

Some commands are not game commands. Queries read game data, change nothing, and are answered at once in any context, even before a character loads and while a screen is open. Each answer carries the query's `id`:

| Query | Answer |
|---|---|
| `SNAPSHOT` | A `SNAPSHOT`. `{"args": {"parts": ["player", "map"]}}` asks for some parts only. |
| `SAVEFILES` | A `SAVEFILES` event with `saves`, the saved characters: each has `name`, which the engine's `-u` option takes; `description`, such as `Tester, L5 Human Warrior, at DL3` or `Tester, dead (Retiring)`; `dead`; `race` and `class`, the living character's race and class, by name; and `current`, true for the savefile this engine uses. The event's own `current` names that savefile even before it is first written, or is empty while a character started with `--new-save` is being made. |
| `OPTIONS` | An `OPTIONS` event. `options` lists every interface, birth, and cheat option: `name`, `description`, `page` (`interface`, `birth`, `cheat`, or `score`), `value`, and `editable`, which is false for birth options outside character creation. The `score` options, such as `score_hear`, mark a character that has used the cheat option of the same name, and are never editable. `numbers` gives `delay_factor` (milliseconds), `hitpoint_warn` (tens of percent, 0 to 9), `lazymove_delay` (hundredths of a second), and `sidebar_mode` (0 left, 1 top, 2 none). `graphics` gives the tile `set` in use (0 for text), its `scale`, and `sets`, each tile set's `id` and `name`. Before a character loads, `loaded` is false, the values are the defaults for a new character, and nothing is editable. |
| `VISUALS` | A `VISUALS` event, for the Tile Editor. `set` names the tile set in use, as its [file of changes](#your-tile-changes) is named, or is empty with text or in headless mode. `lights` names the four kinds of light (`los`, `torch`, `lit`, `dark`) and `directions` the five tiles of a spell effect (`static`, then bolts at `0`, `45`, `90`, and `135` degrees). `entries` lists every monster, object kind, flavor, terrain feature, trap, and spell effect: `kind` (`monster`, `object`, `flavor`, `terrain`, `trap`, or `effect`), `id`, `name`, and `char` and `color`, its letter without tiles. `tiles` gives its tile as `[row, column]` on the sheet, or null for a letter: one for a monster, object, or flavor, one for each of `lights` for terrain and traps, and one for each of `directions` for an effect. Where the player changed a tile, `own` gives the set's own tiles. A flavored object kind says `flavored`: the map shows its flavor's tile instead of its own. |

The other commands that are not game commands:

- `SAVE` saves the game, as Ctrl+S does. It works at the command prompt and while a screen is open.
- `BORG` with `{"args": {"on": true}}` starts the borg, Angband's automatic player, as Ctrl+Z then `z` would, at the command prompt. It marks the character as having used the borg, as the game's own borg command does after its warnings; the client should show that warning itself. `{"on": false}` stops the borg, even in a store, where it ignores keys. While the borg plays, the engine hands no screens or prompts to the client, and when it stops, the engine puts back the options the borg changed. Starting it again is refused with `the borg is still stopping` until the `BORG` event says it stopped. Refused in headless mode, and in an engine built without the borg.
- `LEAVE_STORE` leaves the store, in headless mode.

Settings commands work at the command prompt and while a screen is open, and redraw the screen:

- `SET_OPTION` with `{"args": {"name": "solid_walls", "value": true}}` sets an option, or with a number, as `{"name": "delay_factor", "value": 20}`, a number setting. Birth options change only during character creation. Turning a cheat option on also marks the character as a cheater, as the game's own options menu does.
- `SAVE_OPTION_DEFAULTS` with `{"args": {"page": "interface"}}` or `"birth"` saves that page of the character's options as the defaults for new characters.
- `PANELS` with `{"args": {"sidebar": false, "status_line": false}}` stops the game drawing its sidebar and its status line, for a client that draws them from the [status part](#the-status-part); `true` draws them again. Hiding the sidebar gives its 13 columns to the map, and the map takes the bottom row too. The savefile keeps the player's own sidebar mode, so other front ends still show the sidebar. It works before a character loads too.
- `GRAPHICS` with `{"args": {"set": 2, "scale": 1}}` draws the map with tile set 2, or with text for set 0, and sends `TILESET`. `scale`, 1 to 4, multiplies the cells each tile covers. It works before a character loads too.
- `SET_VISUAL` with `{"args": {"kind": "monster", "id": 12, "tile": [39, 2]}}` gives an entry a tile, at once. `index` picks one of its tiles, by its place in `lights` or `directions`; without it every one changes. `"tile": null` gives it its letter, and `"reset": true` the set's own tile. Refused with text, in headless mode, and before a character loads.
- `SAVE_VISUALS` writes the player's changes to the tile set in use to their [file of changes](#your-tile-changes), or removes the file when nothing is changed.
- `RELOAD_VISUALS` reads the set's files and the file of changes again, dropping changes not saved.

With the text UI running, the engine takes a game command only while the game waits for the player's next command and no menu or screen is open. Otherwise it refuses with `busy`. Keys still work as before.

### Prompts

Each prompt kind matches one of the game's input hooks in `game-input.h`. A `REQUEST` has an `id`, the `prompt` kind, the `text` the game would show (it may be null), and fields for that kind. The `REPLY` repeats the `id` and gives the answer. A reply with `"cancel": true`, or without an answer, cancels the prompt, as Escape would.

| Kind | Extra request fields | Reply |
|---|---|---|
| `check` | none | `{"value": true}` for yes |
| `string` | `default`, `max` (longest answer in bytes) | `{"value": "text"}` |
| `quantity` | `max` | `{"value": n}`, from 0 to `max` |
| `char` | `options`: the characters allowed | `{"value": "y"}` |
| `com` | none | `{"value": "x"}`: one character |
| `direction` | `allow_none` | `{"value": n}`: 1 to 9 as on the number pad; 0 only if `allow_none` |
| `aim` | none | `{"value": n}`: 1 to 9, where 5 means the current target |
| `grid` | `mode` | `{"x": n, "y": n}` |
| `item` | `choices`: objects like the snapshot's gear | `{"item": {"inven": 2}}` |
| `spell_from_book` | `verb`, `choices`: `value`, `name`, `level`, `mana`, `usable` | `{"value": n}`: a spell's `value` |
| `spell` | `verb`, `choices` as above | `{"value": n}` |
| `curse` | `choices`: `value`, `name`, `power` | `{"value": n}` |
| `effect` | `count`, `allow_random` | `{"value": n}` from 0, or `{"random": true}` |
| `confirm_debug` | none | `{"value": true}` |

While the engine waits for a reply it still reads keys and other frames, but it refuses commands with `busy`.

### Screens

A client that claims `screen:NAME` draws that screen itself. When the player opens it, the engine sends a `REQUEST` with `prompt` set to `screen`, `name` set to the screen's name, and for some screens a `view`. The engine swaps the text UI's own command for the screen, so the request comes however the player opens it: by key in either keyset, by a keymap, from the Enter command menu, or from a mouse menu.

| Name | Opened by | `view` |
|---|---|---|
| `options` | `=` | |
| `inventory` | `i`, `e`, `\|` | `inven`, `equip`, or `quiver` |
| `look` | `l` (`x` with the roguelike keys) | |
| `target` | `*` | |
| `knowledge` | `~` | |
| `character` | `C` | |
| `messages` | `Ctrl+P` | |
| `help` | `?` | |
| `map` | `M` | |
| `death` | the character's death or retirement | |
| `store`, `birth` | Reserved; claiming them changes nothing yet | |

The `REPLY` closes the screen:

| Reply | What the engine does |
|---|---|
| `{}` | Goes back to the game. |
| `{"then": COMMAND}` | Goes back to the game and runs the command, which has the same form as a `COMMAND` frame's JSON. A `COMMAND` event answers it. |
| `{"classic": true}` | Shows the text UI's own screen. |
| `{"cancel": true}` | Same as `{}`. |

While a screen is open, the client may send queries and `SAVE`, but no game commands. After the death screen the engine saves the death and exits, as after the text UI's death screen.

**Styled text.** Queries that return the game's colored text, such as descriptions, use styled text: a list of lines, each a list of spans `{"text": "...", "color": n}`, where `color` is a palette index as in [attributes](#attributes-and-colors). Only the game's own line breaks split lines; the client wraps them to fit.

## Headless mode

`fancyband -mnet -- --fd N --headless` runs the game with no text UI and no terms. The engine ignores the terms in the client's `HELLO`, and sends no grid messages. Every prompt is a `REQUEST`, whatever the client claimed, and only `COMMAND` frames drive the game. The engine sends `WAIT` each time it is ready for the next command, and the client should send one command per `WAIT`.

A headless session goes like this:

1. **Character creation.** If the save holds no living character, or the engine was started with `-n`, the engine starts character creation and sends `WAIT` with context `birth`. A snapshot then lists the races and classes. The client sends, for example, `CHOOSE_RACE`, `CHOOSE_CLASS`, `RESET_STATS` with `{"choice": 1}` for the point-based stats, `NAME_CHOICE` with `{"name": "..."}`, and then `ACCEPT_CHARACTER`.
2. **Play.** The engine sends `WAIT` with context `game` before each command.
3. **Stores.** Walking onto a shop entrance enters the store; the engine sends `WAIT` with context `store`. Use `BUY`, `SELL`, `STASH`, and `RETRIEVE`, with `{"store": n}` references for the items for sale, then `LEAVE_STORE`.
4. **The end.** `QUIT` saves and exits with `BYE "quit"`. When the character dies, the engine saves the death and exits with `BYE "dead"`.

## How the session ends

The engine sends `BYE` before it exits. The reason tells the client why:

| Reason | What happened |
|---|---|
| `exit` | The game ended normally, for example after Ctrl+X |
| `quit` | The client sent `QUIT` |
| `dead` | In headless mode, the character died |
| `terminated` | The engine got SIGINT or SIGTERM |
| An error message | The game stopped on an error |

When the client sends `QUIT`, the connection drops, or the engine gets SIGINT or SIGTERM, the engine saves, then exits. It ignores SIGHUP: when an SSH session dies, the client dies with the terminal, and the closed connection makes the engine save. What it saves depends on the game's state:

- **Playing:** it saves the game the same way Ctrl+S does, then exits.
- **Creating a character:** it exits without saving.
- **Dead:** it saves the character as dead, so that dropping the connection on the death screen cannot bring the character back.

While it saves, the engine answers any prompt the game shows with Escape. It exits with status 0 in all three cases.

## Tiles

A client that can show pictures asks for a tile set with the `tiles:N` capability. The numbers come from the engine's `lib/tiles/list.txt`: 1 is the original 8×8 set, 2 Adam Bolt's 16×16, 3 David Gervais' 32×32, and 4 Nomad's 8×16. If the set exists, the engine sends `TILESET` after the terms are open and draws the main term's map with tiles from then on. Otherwise it logs why and keeps drawing text.

- **The sheet.** `TILESET` gives the sheet's absolute path. The client reads it from disk, since it runs on the same machine as the engine. Tile row `r` and column `c` is the rectangle starting at pixel (`c` × tile width, `r` × tile height).
- **Size on screen.** Terminal cells are about twice as tall as they are wide, so a square tile covers two columns and one row. `TILESET` says how many.
- **`PICT`.** Each entry is one tile cell, from (x, y) to the right. Draw the terrain tile, then the tile over it, blending see-through pixels as the SDL2 front end does. Every sheet the game ships has them, whatever its alpha-blending flag says. Fill what stays see-through with black, so the terminal's background does not show. A tile wider than one cell also covers the cells after it, which the engine leaves out: it sends each such tile in a `PICT` of its own. `TEXT` or `WIPE` over a tile's cells replaces it.
- **Text stays text.** The sidebar, the status line, messages, and menus still come as `TEXT`.

Shockbolt's sets, numbers 5 and 6, have tiles twice as tall as the others that draw over the cell above. The engine does not support that yet, so those sets draw cut off.

### Your tile changes

The engine keeps the player's changes to a tile set in the user folder, in `tiles/<set>.prf`, for example `tiles/adam-bolt.prf`. It reads the file after the set's own preference files each time it loads them, so its lines win. The lines are the same kind the sets' files use, such as `monster:scruffy little dog:0x9D:0x9A`, and an effect's lines name it alone, so the effects that share its tiles keep theirs. Sets that share a folder add the end of their preference file's name: Shockbolt's are `shockbolt-dark` and `shockbolt-light`. Changing the tile set drops the changes to the old one that were not saved.

## Debugging with netdump

`netdump` prints frames as text. Build it with `go build ./cmd/netdump` in `client/`.

```
netdump FILE                     print the frames in a captured stream; "-" reads stdin
netdump -connect PATH            talk to an engine started with -- --listen PATH
netdump -run BINARY [ARGS...]    start BINARY -mnet ARGS... and talk to it
```

Add `-caps` with a comma-separated list, such as `-caps events,tiles:2`, to claim capabilities. Add `-screen` to print the main term as text after each `FRESH`, instead of one line per frame. When `netdump` talks to an engine, each line you type is sent as key presses. Plain characters go as they are, and these tokens send other keys: `<esc>`, `<enter>`, `<tab>`, `<bs>`, `<up>`, `<down>`, `<left>`, `<right>`, `<^X>` for any control letter, `<kp8>` for a number-pad digit, and `<quit>` for `QUIT`.
