# Playing Fancyband

Fancyband is a variant of Angband 4.2.6 that you play in a terminal. It is two programs: `fancyband`, the game, and `fancyband-tui`, which draws the game in your terminal and passes it your keys. You start `fancyband-tui`, and it starts the game.

## Start playing

Unpack the release and run the client from its folder:

```
tar xzf fancyband-*-linux-x86_64.tar.gz
cd fancyband-*-linux-x86_64
./fancyband-tui
```

A start menu opens over the title screen:

- **Continue** loads the character you played last.
- **New character** makes a new one. Its name names its savefile, so it never replaces another character. If a character with that name already has a save, the game asks first.
- **Load a character** lists your other characters.
- **Quit** leaves.

Choose with the arrow keys and Enter, or click an entry. In the list of characters, which shows each one's tile when the map uses tiles, one click selects a character and a double click loads it; the **Load** and **Cancel** buttons do the same as Enter and Escape, and `/` searches.

You can also pick a character, or start a new one, by putting game options after `--`:

| Command | What it does |
|---|---|
| `./fancyband-tui -- -n` | Open the start menu with New character selected. |
| `./fancyband-tui -- -uGandalf` | Open the start menu with Continue set to the character called Gandalf. |

## The game menu

Press **Escape** or **F10** while the game waits for your next command to open the game menu:

- **Resume** goes back to the game.
- **Save** saves the game.
- **Load a character** saves this character and loads another, or makes a new one.
- **Options** opens the options screen. See [options](#options).
- **Tile Editor** gives things other tiles. See [change the tiles](#change-the-tiles).
- **Borg** turns on Angband's automatic player, the borg. It plays the character until you press **Escape** or **F10**, which stops it and opens this menu. The first time for each character, a warning explains that using the borg keeps the character off the high score list. The map keeps its tiles while the borg plays. When the borg stops, your options are as you left them.
- **Save & Quit** saves and quits.

Escape still cancels prompts and menus, as in Angband. Just after a run or a rest ends, Escape goes to the game for half a second, so pressing it to stop the run does not open the menu by surprise. F10 always opens the menu.

When a character dies, a menu offers a new character, another character, a look back at the dead one through the game's own screens, or Quit.

Press **Ctrl+C** at any time to save and quit. The game's own key for that, **Ctrl+X**, works too. If the terminal closes or the connection drops, the game saves before it exits.

## Options

Press `=`, or choose **Options** in the game menu or the start menu. The screen has five pages; press **Tab** or a number from 1 to 5 to move between them:

| Page | What it sets |
|---|---|
| Interface | The game's interface options, such as `solid_walls`, each with a checkbox |
| Numbers | The delay for animations, the hit point warning, and the wait for a second key on diagonal moves |
| Birth | The birth options. They show here, but change only while you make a character |
| Cheat | The cheat options. Turning one on asks first, because it marks the character as a cheater for good |
| Display | Fancyband's own settings: the tile set, the tile size, animations, the side panels, and which screens are Fancyband's own |

Move with the arrow keys, and press **Space** or **Enter** to change a checkbox or **Left** and **Right** to change a number. The mouse works too. Changes take effect at once.

**Save this page as the defaults for new characters**, on the Interface and Birth pages, does what it says. **More options**, on the Interface page, opens the game's own options menu, for keymaps, colors, what to ignore, and the rest. The Display page saves itself in `fancyband-tui.json`.

Before a character loads, the options screen opens on the Display page; the game's options show the defaults for new characters.

## What your terminal needs

The window must be at least 80 columns by 24 rows. With more room, the client adds a panel: a side panel with the message log and the list of monsters you can see when the window is wider than about 105 columns, or a bottom panel with the message log when it is taller than about 31 rows. Add `-subwindows=false` to turn the panels off.

The client works in any modern terminal, over SSH too. Kitty, Konsole, and macOS Terminal have been tried.

## Draw the map with pictures

The map can show tiles, small pictures for each monster, item, and kind of ground, in place of letters. Pick a tile set and a size on the Display page of the [options](#options), while you play; the client remembers your choice. Sizes that would leave too little of the map in the window are not used: the map drops to a smaller size and goes back up when the window grows. You can also pick a set with `-tiles`, which wins over the options for that session:

| Option | Tile set |
|---|---|
| `-tiles 1` | The original 8×8 tiles |
| `-tiles 2` | Adam Bolt's 16×16 tiles |
| `-tiles 3` | David Gervais' 32×32 tiles |
| `-tiles 4` | Nomad's 8×16 tiles |
| `-tiles 5`, `-tiles 6` | Shockbolt's 64×64 tiles, dark and light |

A few of Shockbolt's monsters are twice as tall as a grid and reach into the grid above. The client does not draw those right yet.

Tiles need a terminal that can show pictures. Kitty and Konsole both do, with Kitty's image protocol, and the client picks it in both. Konsole can also show an older protocol called Sixel, but with Sixel creatures and the map do not move smoothly. If pictures do not show in another terminal, tell the client which to try:

| Terminal | Option |
|---|---|
| Kitty | `-graphics kitty` |
| Konsole | `-graphics kitty` |

If the terminal cannot show pictures, or you run inside tmux, which drops them, the client draws letters instead.

Tiles cost more to send than letters: about 3 KB a move with Kitty's protocol and about 90 KB with Sixel, against a few hundred bytes for letters. Over a slow SSH link, prefer letters, or Kitty.

Lava ripples between red and yellow. Turn off **Animations** on the Display page, or add `-animate=false`, to keep it still.

## Change the tiles

The Tile Editor lets you give anything another tile, or give a tile to something that shows as a letter because its set has none for it. Press F2 while the game waits for a command, or choose **Tile Editor** in the [game menu](#the-game-menu). It needs a tile set.

The editor has a tab for each kind of thing: monsters, objects, flavors, terrain, traps, and effects, which are the bolts, balls, and explosions of spells. Each row shows the letter, the tile, and the name.

- **Tab** moves to the next tab, and **/** searches by name.
- **m** shows only the things that lack a tile.
- **Enter** opens the tile sheet. Move the cursor with the arrow keys or click a tile, and press **Enter** or click it again to use it. The panel beside the sheet lists what already uses the tile under the cursor.
- Terrain and traps can have a different tile in each kind of light, and an effect has a tile for each bolt direction and one for balls and explosions. **Tab** in the sheet picks which one changes. For an effect, **f** sets all four bolt directions at once from the tile under the cursor and the three after it.
- **u** puts back the set's own tile.
- **s** saves your changes.

The map changes as soon as you pick a tile. If you leave without saving, the editor asks whether to save or to put back what you saved before.

Your changes are kept for each tile set, in `tiles/` in your [Fancyband folder](#where-fancyband-keeps-your-files), for example `tiles/adam-bolt.prf`. Other Angband front ends do not read them. Changing to another tile set drops the changes you have not saved.

## Keys

Fancyband uses Angband's keys. These are the ones you need first:

| Key | What it does |
|---|---|
| Arrow keys or number pad | Move, or attack a monster in the way |
| `.` then a direction | Run |
| `<` and `>` | Go up or down stairs |
| `g` | Pick up |
| `i`, `e` | Show your pack, show what you wear |
| `w`, `t` | Wear or wield, take off |
| `q`, `r`, `E` | Drink a potion, read a scroll, eat |
| `f`, `v` | Fire a missile, throw |
| `a`, `u`, `z` | Aim a wand, use a staff, zap a rod |
| `b`, `m` | Browse a book, cast a spell |
| `R` | Rest |
| `o`, `c`, `D`, `T` | Open, close, disarm, tunnel |
| `l` | Look around |
| `C` | Show your character |
| `Ctrl+P` | Show past messages |
| `Enter` | A menu of every command |
| `=` | Options, including the roguelike keys that move with `h`, `j`, `k`, `l` |
| `?` | The game's help, with every command |
| `Escape` or `F10` | The game menu |
| `Ctrl+X` or `Ctrl+C` | Save and quit |

Clicking the map with the mouse walks there.

## Where Fancyband keeps your files

Everything the game writes goes in `~/.angband/Fancyband`:

| Folder or file | What it holds |
|---|---|
| `save/` | Your characters |
| `scores/` | The high score list |
| `panic/` | Emergency saves, if the game ever crashes |
| `fancyband-tui.json` | The client's settings, such as the character you played last |
| `tiles/` | Your changes to each tile set, from the Tile Editor |
| `borg.txt` | The borg's settings, copied there the first time you turn the borg on |
| `fancyband-tui.log`, `fancyband-engine.log` | What the client and the game logged in the last session |

Because of this, you can unpack a newer release next to an old one, or in its place, and keep your characters. Add `-log-dir FOLDER` to write the logs somewhere else.

If something goes wrong, the two log files usually say why.

## Licenses

The `licenses/` folder holds every license that applies:

| File | Covers |
|---|---|
| `angband-copying.rst` | The game, under the GNU GPL version 2 or the Angband licence, and the tile sets' own terms |
| `GPL-2.0.txt` | The text of the GNU GPL version 2 |
| `limoni-LICENSE`, `limoni-FORK.md` | Limoni, the terminal library in `fancyband-tui`, under Apache 2.0, and what Fancyband changed in it |
| `golang.org-x-sys-LICENSE`, `go-LICENSE` | Go libraries built into `fancyband-tui` |

The source code for this release is in the matching `fancyband-*-src.tar.gz`.
