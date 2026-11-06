# Playing Fancyband

Fancyband is a variant of Angband 4.2.6 that you play in a terminal. It is two programs: `fancyband`, which draws the game in your terminal and passes it your keys, and `bin/engine`, the game itself. You start `fancyband`, and it starts the engine.

## Start playing

Unpack the release and run the client from its folder:

```
tar xzf fancyband-*-linux-x86_64.tar.gz
cd fancyband-*-linux-x86_64
./fancyband
```

A start menu opens under the title screen: FANCYBAND in large letters, with fire burning above them, and the game's quote and credits. The fire moves while **Animations** is on in the options, and stands still when it is off. A small window leaves out the quote and the credits to make room for the menu.

The start menu offers:

- **Continue** loads the character you played last.
- **New character** makes a new one. Its name names its savefile, so it never replaces another character. If a character with that name already has a save, the game asks first.
- **Load a character** lists your other characters.
- **Options** opens the options screen. See [options](#options).
- **Tile Editor (User)** changes the tiles for every character you play. See [change the tiles](#change-the-tiles).
- **Quit** leaves.

Choose with the arrow keys and Enter, or click an entry. A long list also scrolls with the mouse wheel and **Page Up** and **Page Down**. In the list of characters, which shows each one's tile when the map uses tiles, one click selects a character and a double click loads it; the **Load** and **Cancel** buttons do the same as Enter and Escape, and `/` searches.

You can also pick a character, or start a new one, by putting game options after `--`:

| Command | What it does |
|---|---|
| `./fancyband -- -n` | Open the start menu with New character selected. |
| `./fancyband -- -uGandalf` | Open the start menu with Continue set to the character called Gandalf. |

## Make a character

New character opens Fancyband's own birth screen. It goes a step at a time, and the steps along the top show where you are; click one you have done to go back to it, or press **Escape** to go back a step.

1. **Race** and **Class.** Move through the list; the panel on the right shows what each is good at: stat changes, skills, and abilities. Press **Enter**, double-click, or click **Next** to choose and go on.
2. **Stats.** The table shows each stat, the race's and class's changes, and the result. The game suggests a set of point-based stats. Select a stat with the up and down arrows, and press the right arrow or `+` to spend a point on it, or the left arrow or `-` to take one back; the **+** and **-** buttons do the same. Press `r` to roll random stats instead, `p` for the previous roll, and `b` to go back to the suggested points.
3. **Name.** Type a name. The first key replaces the suggested one. If a savefile already has that name, the game asks before it replaces it.
4. **History.** The game writes a short history for your character. Press `e` to change it.
5. **Begin.** A last look at the character. Press **Enter** to start playing, or `s` to start over.

Press `=` at any step for the birth options. After a character dies, the game may first offer a new one just like it: `y` uses it as is, `c` changes the name first, and `n` makes a different one.

## Shops

Walk into a shop's entrance, a number on the town map, to open the store screen. It shows what the store sells on the left, with prices, and what you carry on the right, with what the store would pay. The panel under them describes the selected item, and the game's messages, such as what you just bought, show above the buttons.

- **Tab**, or the left and right arrows, moves between the two lists.
- `p`, or **Enter** in the store's list, buys the selected item. For more than one, a dialog asks how many, then the game asks you to confirm the price. `o` buys just one.
- `d`, or **Enter** in your list, sells the selected item. Items the store does not buy are dimmed, and trying says why. With the birth option that makes stores pay nothing, the buttons say **Give**.
- In your home, the same keys take items out and drop them in, with no prices.
- **Escape** leaves the shop.

The lists scroll with the arrows, **Page Up** and **Page Down**, and the mouse wheel; the description scrolls with **Shift** and the arrows or page keys. The birth screen and the store screen can each go back to the game's own version on the Display page of the options.

## The game menu

Press **Escape** or **F10** while the game waits for your next command to open the game menu:

- **Resume** goes back to the game.
- **Save** saves the game.
- **Load a character** saves this character and loads another, or makes a new one.
- **Options** opens the options screen. See [options](#options).
- **Tile Editor (Character)** gives things other tiles for this character. See [change the tiles](#change-the-tiles).
- **Borg** turns on Angband's automatic player, the borg. It plays the character until you press **Escape** or **F10**, which stops it and opens this menu. The first time for each character, a warning explains that using the borg keeps the character off the high score list. The map keeps its tiles while the borg plays. When the borg stops, your options are as you left them.
- **Save & Quit** saves and quits.

Escape still cancels prompts and menus, as in Angband. Just after a run or a rest ends, Escape goes to the game for half a second, so pressing it to stop the run does not open the menu by surprise. F10 always opens the menu.

When a character dies, a tombstone shows what the game engraves on it: your character's name, title, class, level, experience, and gold, what killed them and where, and the date. A retired character's stone says so. A character who won the game gets a crown instead. A menu beside it offers:

- **New character like NAME**, as the game's own New Game does. Character creation starts with a quick start: a character like the dead one, which you can use as is, rename, or set aside to make a new one. A name that ends in a Roman numeral counts up, so Tester II's successor is Tester III. The new character takes the dead one's savefile.
- **New character**, in a savefile of its own, from the start.
- **Load a character.**
- A look at the dead character's items, character sheet, messages, and knowledge, and the hall of fame. The character sheet also saves a character dump.
- **Quit.**

Press **Ctrl+C** at any time to save and quit. The game's own key for that, **Ctrl+X**, works too. If the terminal closes or the connection drops, the game saves before it exits.

## Items and spells

Fancyband draws the inventory and the game's questions about items and spells itself.

**The inventory.** Press `i` for the pack, `e` for the equipment, or `|` for the quiver. They are three tabs on one screen: press **Tab** to move between them, `/` to switch between the pack and the equipment, or `|` for the quiver. Each item shows its tile or letter, its name, and its weight, and the panel on the right describes the selected one. The bottom of the screen shows how much you carry and how much more you can carry before it slows you down.

To use an item, press its letter, or select it and press **Enter**. A menu lists what you can do with it, such as Quaff, Equip, Take off, Drop, Throw, or Inscribe, each on the game's own key for it. So `i`, then `b`, then `q` drinks the potion in slot b, as in Angband. Choices you cannot make now are dimmed. **Browse** shows a book's spells over the inventory. Inscriptions such as `!d` still ask before you drop the item.

**Which item?** When a command needs an item, such as `q` to quaff, a list shows only the items that fit, with a tab for each place they are in: the pack, the equipment, the quiver, or the floor. Press an item's letter to choose it, or a digit for an item inscribed with it, such as `@q1`. The `/`, `|`, and `-` keys go to the pack or equipment, the quiver, and the floor. If nothing fits, the game says so and asks nothing.

**Spells.** Casting, studying, and browsing a book show its spells under column headings, each with its level, mana, chance to fail, and the game's note on it, such as "untried" or its damage. The panel on the right describes the selected spell. Spells you cannot choose now are dimmed. Press a spell's letter to choose it.

**Small questions** come as small dialogs:

- **How many?** Type a number, or change it with the arrow keys, **Page Up** and **Page Down** (by 10), the mouse wheel, or the **[-]** and **[+]** buttons. **End** sets the number to all of them, and the **All** button takes all of them at once.
- **Yes or no?** Press `y` or `n`. **Escape** means no, and No is selected at first.
- **Which direction?** Press a digit, an arrow key, or a roguelike key such as `h`, or click a direction.

**Keymaps still work.** When a keymap types the answer to a question, as a keymap for `q1` does, the game's own prompt takes it, as it would without Fancyband.

The description panel scrolls with the mouse wheel, or with **Shift** and the arrow keys or **Page Up** and **Page Down**. Each of these screens can go back to the game's own version on the Display page of the options; the change takes effect when the game next starts.

## Look and target

Fancyband draws look mode, target mode, and the question of where to aim itself. Each is a list of what you can see, nearest first, with a description of the selected thing beside it. As you move through the list, a marker shows the thing on the map: a highlighted letter, or a yellow frame round the tile. The list moves to the top or the bottom of the window to keep out of the marker's way.

**Look around.** Press `l` (`x` with the roguelike keys), or choose Look in the game's mouse menu. The list shows you, the monsters, objects, traps, and interesting places, such as doors and shop entrances, that the map shows now. Move with the arrow keys, **Page Up** and **Page Down**, the mouse wheel, or **Space** and `-`, as in Angband. Click something on the map to select it, or click any other grid to look at it.

- `t` or **Enter** targets the selected monster, or the selected place.
- `g` travels there.
- **Escape** or `q` goes back to the game.

**Pick a target.** Press `*`. The list shows only the monsters you can target, and the one you targeted last is selected. Press **Enter** or `t` to target the selected one.

**Aim.** When a spell, wand, or missile needs a direction, the list shows what you can target:

- **Enter**, `t`, or `5` fires at the selected monster.
- `'` fires at the closest.
- A digit, or a roguelike key such as `h`, fires in that direction.
- **Escape** cancels.

With nothing to target, the direction dialog asks instead. The game's `use_old_target` option still fires at your target without asking.

**See what is around you.** Press `[` for the monsters you can see or sense, or `]` for the items you can see or know about. Each row shows the thing's tile, or its letter in text mode, then its name and how far away it is, such as `3 N 4 W`. In the monster list, press `x` to sort by the experience each monster gives. The side panel's monster list shows the same tiles or letters. **Escape** closes the list.

**Read about a monster in the side panel.** Click a monster in the list of monsters you can see, in the side panel, to read its description. The description scrolls with the arrow keys, **Page Up** and **Page Down**, and the mouse wheel. **Escape** closes it.

These screens can go back to the game's own versions on the Display page of the options.

## Knowledge, your character, messages, and help

**What you know.** Press `~`, or `/` to open it on the monsters. Each section of the game's knowledge menu is a tab: objects, runes, artifacts, ego items, monsters, terrain features, traps, shapes, the stores' stock, the hall of fame, your character's history, and the equipment comparison. Sections you know nothing about yet are left out. Entries come in the game's groups, such as Canines or Potions, with their letter or tile and a note, such as how many of a monster you have killed. The description of the selected entry shows beside the list. With tiles, the entry's tile follows the name at the top of its description. In text mode, a monster's description shows its letter, such as `('h')`.

- **Tab** and **Shift+Tab** change the section. `/` searches it.
- `t` opens the Tile Editor on the selected monster, object, or kind of terrain. Close the editor to come back.
- The equipment comparison opens a table of everything you could wear against everything the game compares. See [compare equipment](#compare-equipment).

**Your character.** Press `C`. The sheet has three tabs: a summary with your stats and history, your combat skills and other skills, and the table of resistances and abilities for each thing you wear. That table holds every row in one column, under the headings Stats, Resistances, Abilities, Hindrances, and Modifiers, and scrolls while its headings stay put. Each slot has a column of its own, three cells wide. Each column is one equipment slot, headed by the letter the game gives the slot: `a` for your weapon, `b` your bow, and so on to `p` your boots, then `@` for you. When the map uses tiles, the headings show tiles instead: what you wear in each slot, and your own tile in place of `@`. A slot with nothing in it is blank. `c` changes your name, and `f` saves the whole sheet to a text file, named after your character, in your user folder.

**Past messages.** Press `Ctrl+P`. The newest messages are at the bottom, in the colors of the message log. `/` keeps only the messages with some text in them. `f` shows only one kind of message, such as hits or kills, starting with the most common kind; press `f` again for the next kind, and `F` to go back.

**Help.** Press `?`. The game's help files open with their headings in color. A letter, such as `a` for the list of commands, or a click on its line, opens a file; **Escape** goes back.

All of these scroll with the arrow keys, **Page Up** and **Page Down**, and the mouse wheel. **Escape** closes them.

When your character dies, the death menu opens these too: your character sheet, your messages, what you know, and the hall of fame.

These screens can go back to the game's own versions on the Display page of the options.

### Compare equipment

The equipment comparison is the last tab of the knowledge screen. Each row is an item, with its tile or letter: what you wear, what you carry, what lies at your feet, what you keep at home, and what the stores sell. Each column is something the game compares, under a short name such as **Acid**, **Free act**, or **Speed**: resistances, abilities, hindrances, other modifiers such as speed and tunneling, and stat bonuses. Resistances, abilities, and hindrances use the game's own symbols. Modifiers and stats show their numbers, such as `+2`. A `?` marks a rune you have not learned yet, and a dim dot means nothing. The top row, marked with your tile or `@`, is you with what you wear now.

- **Tabs** choose which items show: **Yours** (everything but the stores' wares), **In stores**, **Everything**, or **Carried** (what you wear and carry). Press `c` or **Tab**, or click one.
- **Move** with the arrow keys, the page keys, or the mouse wheel. **Left** and **Right** move between the columns, and the line under the table gives the full name of the column you are on, such as **Fire (resists)**. Click a column's name to go to it.
- **When the columns do not all fit,** they scroll sideways while the item names stay put. Moving past the edge scrolls them, as do **Shift** and the wheel, the sideways wheel, and the scroll bar under the table. `v`, or **Shift** and **Right**, jumps to the next group of columns, and **Shift** and **Left** to the one before.
- **Filter** with `q`, which keeps the items good in that column: those that resist, have the ability, are free of the hindrance, or raise the modifier. `!` keeps the others. The same key again turns the filter off.
- **Sort** with `s`, best first by that column, and again to go back to the game's order. `r` reverses the order, and `R` resets everything.
- **Describe** an item with **Enter** or a double click. To compare two items, press `x` on one and `x` on the other: their descriptions show one after the other.

A switch on the Display tab brings back the game's own comparison screen.

## The level map

Press `M` to see the whole level as you remember it. The cursor starts on you, and the line under the map says what it is on: you, a staircase, or a shop.

- **Pan** with the arrow keys, or **Page Up** and **Page Down** for a screenful. Hold **Shift** to pan 10 times as far. The mouse wheel pans too, and **Shift** and the wheel go sideways. Panning leaves the cursor where it is. When the whole level fits on the screen, there is nothing to pan.
- **Move the cursor** with the number keys or `h`, `j`, `k`, `l`, `y`, `u`, `b`, `n`. Use the capital letters to move 10 grids. The map follows the cursor.
- **Jump** to the nearest staircase with `<` or `>`, and to the next one by pressing it again. With tiles, the legend under the map shows the two staircases' tiles, up then down, in place of these two keys. `s` does the same for shops, and `@` or **Home** comes back to you.
- **Travel** with **Enter**, or click a grid. The map closes and your character walks there, as a click on the map does. If the game knows no way there, a note says so. If you panned the cursor out of view, **Enter** brings it back first, and a second **Enter** travels.
- **Zoom** with `+` and `-`, or **Ctrl** and the mouse wheel. In text mode the map has two sizes: a letter a grid, and an overview with two grids in each cell, where stairs, shops, and you still show as letters. With tiles it has three: the map's own tile size, half that, and a quarter. The map opens at a letter a grid in text mode, and at a quarter with tiles. Small tiles have colored frames: yellow for you, green for up stairs, blue for down stairs, and orange for shops. The cursor is a white frame.

**Escape** closes the map. The Display page of the options can bring back the game's own map.

## Read what a name means

Many screens underline the names the game has rules for: races, classes, stats, skills, abilities such as Hold Life, elements such as fire, curses, and status effects such as Blind. Click an underlined name, or rest the mouse on it for half a second, and a box opens beside it to say what it does.

- **Close the box** with **Escape**, a click somewhere else, or a second click on the name. A box the mouse opened closes when the mouse moves away.
- **Scroll a long box** with the arrow keys and **Page Up** and **Page Down**.
- **Use the keyboard:** press `'` to list the names the screen shows, and choose one to open its box.

You find these names on the birth screen's race and class panels, which also show the game manual's paragraph on each race and class; in item and spell descriptions, in the inventory, the stores, and the item prompts; on the knowledge, look, and character screens; in monster descriptions; in the equipment comparison's column names; on the stats bar's stats; and in the status line's effects. Names inside list rows and buttons are not underlined, since a click there chooses the row.

The text is the game's own: the one-line descriptions in its data files, and, for races, classes, stats, and skills, which the data files do not describe, the game's manual.

Opening a box when the mouse rests needs the terminal to report every move of the mouse, which adds a little traffic over SSH. To turn that off, clear **Descriptions under the mouse** on the Display page of the [options](#options). Clicks still work.

## Options

Press `=`, or choose **Options** in the game menu or the start menu. The screen has five pages; press **Tab** or a number from 1 to 5 to move between them:

| Page | What it sets |
|---|---|
| Interface | The game's interface options, such as `solid_walls`, each with a checkbox |
| Numbers | The delay for animations, the hit point warning, and the wait for a second key on diagonal moves |
| Birth | The birth options. They show here, but change only while you make a character |
| Cheat | The cheat options. Turning one on asks first, because it marks the character as a cheater for good |
| Display | Fancyband's own settings: the tile set, the tile size, animations, flipping creatures, the side panels, highlighting and descriptions under the mouse, and which screens are Fancyband's own |

Move with the arrow keys, or a page at a time with **Page Up** and **Page Down**, and press **Space** or **Enter** to change a checkbox or **Left** and **Right** to change a number. The mouse works too, and its wheel scrolls the list. Changes take effect at once.

**Save this page as the defaults for new characters**, on the Interface and Birth pages, does what it says. **More options**, on the Interface page, opens the game's own options menu, for keymaps, colors, what to ignore, and the rest. The Display page saves itself in `fancyband-tui.json`.

**Highlight under the mouse**, on the Display page, lights up the button, row, or tab under the mouse pointer in Fancyband's own screens, such as the menus, the options, and the Tile Editor. It is off by default, because the terminal then reports every move of the mouse, which adds a little traffic over SSH.

**Use the game's own screens.** Each screen Fancyband draws itself has a switch on the Display page, such as **Native inventory screen** or **Native level map**. Clear one to get the game's classic screen back, as Angband draws it. The switches cover the splash screen, the death screen, the command menu, the inventory, the stores, character creation, the item and spell prompts, the small dialogs, look and target mode, the knowledge screen, the equipment comparison, the character sheet, the message history, the help, the level map, and the monster and item lists. A change takes effect when the game next starts. **Native stats bar** brings back the game's own sidebar and status line, and takes effect at once.

Before a character loads, the options screen opens on the Display page; the game's options show the defaults for new characters.

## What your terminal needs

The window must be at least 80 columns by 24 rows. With more room, the client adds a panel: a side panel with the message log and the list of monsters you can see when the window is wider than about 105 columns, or a bottom panel with the message log when it is taller than about 31 rows. Add `-subwindows=false` to turn the panels off.

**The stats bar.** During a game, a bar beside the map shows your character: level, gold, what you wear, stats, armor, and gauges for HP and SP. It is hidden on the start screen, in character creation, and behind the death menu, where the screen has the whole width. When you fight a monster, its name and a gauge of its health show under your own gauges, set apart from them by a blank row. The gauge is in the game's color for the monster's health, and shows `?` when you cannot see the monster.

The client works in any modern terminal, over SSH too. Kitty, Konsole, iTerm2, and macOS Terminal have been tried.

## Draw the map with pictures

The map can show tiles, small pictures for each monster, item, and kind of ground, in place of letters. A monster or your character faces the way it last stepped: its tile flips when it steps left and flips back when it steps right, assuming the tile faces right on the sheet. A step straight up or down, or a teleport, keeps its facing. Facing isn't saved, so everything faces right again when you load a game. Clear **Directional Flipping** on the Display page to keep every tile as it is on the sheet; the change takes effect at once. Pick a tile set and a size on the Display page of the [options](#options), while you play; the client remembers your choice. Sizes that would leave too little of the map in the window are not used: the map drops to a smaller size and goes back up when the window grows. You can also pick a set with `-tiles`, which wins over the options for that session:

| Option | Tile set |
|---|---|
| `-tiles 1` | The original 8×8 tiles |
| `-tiles 2` | Adam Bolt's 16×16 tiles |
| `-tiles 3` | David Gervais' 32×32 tiles |
| `-tiles 4` | Nomad's 8×16 tiles |
| `-tiles 5`, `-tiles 6` | Shockbolt's 64×64 tiles, dark and light |

**Soften the edges of light.** **Light edges**, on the Display page, smooths the steps where the floor's light changes, such as along your line of sight, and where floor you know meets grids you have not seen, as in the town at night. It only changes the floor's look, with tiles. **Corner blending** turns the steps into a smooth slope, **Diagonal cuts** turn them into slanted lines, and **Dithered fade** is corner blending drawn as a pixel pattern. It is off by default. It costs more data, most of all with Sixel and with **Dithered fade**, and floors with animated filters are left as they are.

A few of Shockbolt's monsters are twice as tall as a grid and reach into the grid above. The client does not draw those right yet.

Tiles need a terminal that can show pictures. Kitty, Konsole, and iTerm2 all do, with Kitty's image protocol, and the client picks it in all three. iTerm2's own protocol is much slower, especially over SSH; `-graphics iterm2` chooses it if Kitty's does not show. Konsole can also show an older protocol called Sixel, but with Sixel creatures and the map do not move smoothly. If pictures do not show in another terminal, tell the client which to try:

| Terminal | Option |
|---|---|
| Kitty | `-graphics kitty` |
| Konsole | `-graphics kitty` |
| iTerm2 | `-graphics kitty` |

If the terminal cannot show pictures, or you run inside tmux, which drops them, the client draws letters instead.

Tiles cost more to send than letters, which take a few hundred bytes a move. We measured a 200-move walk at tile size 2 in a 180 by 50 window. A typical move sent about 3 KB with Kitty's protocol and about 8 KB with Sixel or iTerm2's. The busiest moves sent up to 20 KB with Kitty's, 40 KB with iTerm2's, and 60 KB with Sixel. Over a slow SSH link, prefer letters, or Kitty.

With tiles, you can animate lava, or anything else, with a filter in the [Tile Editor](#give-a-tile-a-filter). Turn off **Animations** on the Display page, or add `-animate=false`, to keep everything still.

## Change the tiles

The Tile Editor lets you give anything another tile, or give a tile to something that shows as a letter because its set has none for it. It needs a tile set. There are two:

- **For this character:** press F2 while the game waits for a command, or choose **Tile Editor (Character)** in the [game menu](#the-game-menu). Your changes are for this character only.
- **For every character:** choose **Tile Editor (User)** in the [start menu](#start-playing). Your changes are for every character you play, except where a character has their own.

For each tile, the map shows the character's own change if there is one, else your change for every character, else Fancyband's global tiles, which every player on the computer gets, else the tile set's own. Each works the same way.

The editor fills the window, in three columns:

1. **The list** of everything that can take a tile, with a tab for each kind of thing: the player, monsters, objects, flavors, terrain, traps, and effects, which are the bolts, balls, and explosions of spells. Each row shows the letter, the tile, and the name.
2. **The preview** of the selected thing's tile, three times the size it is on the map, with its filter, on the tile set's floor. Its details are under it.
3. **The tile sheet, the filter, and the symbol,** as three tabs. While you move through the list, this column follows: the sheet shows where the selected thing's tile is, the filter tab shows its filter, and the symbol tab shows its letter.

The column with a gold border has the keys. Click a column to give it the keys, or press **Esc** in the third column to go back to the list. In a narrow window, the third column takes the preview's place while you use it.

- **Tab** moves to the next tab of the list, and **/** searches by name.
- **m**, or the **Missing only** checkbox, shows only the things that lack a tile.
- **Enter** gives the tile sheet the keys. The sheet shows each tile at your tile size, on the tile set's floor. Click a tile to use it, or move the cursor with the arrow keys and press **Enter**. The panel beside or under the sheet lists what already uses the tile under the cursor.
- **Scroll the sheet** with the mouse wheel, and sideways with **Shift** and the wheel. When the sheet does not fit, scroll bars show at its right and under it: click an arrow to move one tile, click the bar beside the thumb to move a page, or drag the thumb.
- **Zoom the sheet** with `+` and `-`, **Ctrl** and the wheel, or the **[-]** and **[+]** buttons in the panel, from size 1 to 4. `0` goes back to your map's tile size. The client remembers the zoom.
- Terrain and traps can have a different tile in each kind of light, and an effect has a tile for each bolt direction and one for balls and explosions. **Tab** in the sheet picks which one changes. For an effect, **f** sets all four bolt directions at once from the tile under the cursor and the three after it.
- **f** opens the filter for the thing, described [below](#give-a-tile-a-filter).
- **u** takes your change away, and its filter, so the tile is again the one you had before you changed it here.
- **s** saves your changes.

A row's note says where its tile comes from: **changed** when you changed it here, and **user** when it comes from your changes for every character. A tile from Fancyband's global tiles has no note. Its details say where every tile comes from, and in the editor for every character, they say when this character has their own tile instead.

**Your own tile.** Your character's tile comes from the first of three that they have:

1. **Their own.** In the editor for this character, the **Player** tab starts with `<player>`, which is this character alone, such as `<player> (Flor)`. A tile you give it is for this character only, even if you make another of the same race and class. Until you give one, the row shows the tile they have now, marked with where it comes from, such as "uses Half-Troll Warrior".
2. **Their race and class's.** Every race and class is listed under a heading for each class. A tile set can give each one a tile of its own, and a tile you give one is for every character of that race and class without a tile of their own.
3. **The default.** A race and class without a tile of its own, marked "uses the default tile", shows the tile set's tile for any player: in Adam Bolt's set, the Human Warrior picture. The editor does not list the default itself. Adam Bolt's set, for example, has no tiles of its own for druids, necromancers, and blackguards, so **Missing only** lists them. Every race and class has the same letter, so the **Symbol** tab changes it for all of them at once.

The buttons under the list do the same with the mouse: **Undo**, **Save**, and **Close**, which leaves the editor as **Esc** does. The tabs of the third column open the sheet, the filter, and the symbol.

Every list in the editor, and the sheet, scrolls with the mouse wheel, the arrow keys, and **Page Up** and **Page Down**. The wheel scrolls whatever is under the pointer, even when another column has the keys.

The map changes as soon as you pick a tile. If you leave without saving, the editor asks whether to save or to put back what you saved before.

Your changes are kept for each tile set in your [Fancyband folder](#where-fancyband-keeps-your-files): those for every character in `tiles/`, for example `tiles/adam-bolt.prf`, with your filters beside it in `tiles/adam-bolt.json`, and each character's in `characters/NAME/tiles/`. Other Angband front ends do not read them. Changing to another tile set drops the changes you have not saved.

**Fancyband's global tiles** are for every player on the computer. They ship with the game, in `lib/tiles/fancyband/`. To change them, run the client from a Fancyband source tree with `-dev`. The start menu then adds **Tile Editor (Global)**, which shows the tiles as a new player sees them and saves in the source tree's `engine/lib/tiles/fancyband/`, and the game menu adds **Tile Editor (User)**. Each save also updates the engine's own copy of that folder, so a game without `-dev` shows the new global tiles at the next start. Release builds ship whatever the source tree holds.

### Change the letter for text mode

When the map draws letters instead of tiles, each thing shows as a letter in a color, such as a white `@` for you. The **Symbol** tab changes them. Select a row, click **Symbol**, and type the new letter, number, or sign. Use **Left** and **Right** on **Color**, or click one of the colors under the list, which show the letter in each. The preview shows the letter beside **As a letter**, and **Take the change away** undoes the change.

In text mode an effect draws `*` for a ball or a line along its path, so an effect takes only a color.

**s** saves your symbols with your tiles. They work for every tile set, and the map uses them whenever it draws letters. Those for every character are kept in `symbols.prf` in your [Fancyband folder](#where-fancyband-keeps-your-files), and each character's in `characters/NAME/symbols.prf`.

### Give a tile a filter

A filter changes how one thing's tile looks, without changing the tile set. Select a row in the Tile Editor and press **f**, or click the **Filter** tab. The third column lists the settings, and the preview shows the tile through the filter. The map changes as you go.

| Setting | What it does |
|---|---|
| Flip Horizontal | Mirrors the tile left to right. **Directional Flipping** expects every tile to face right, so flip one drawn facing left and it turns the right way as the monster moves. The flip comes before every other setting, and the animation never changes it |
| Hue | Turns colors around the color wheel: 120° makes red green, 240° makes it blue |
| Saturation | Makes colors richer, or grayer |
| Lightness | Makes the tile brighter or darker |
| Opacity | How much the tile covers what is under it. A ghost at 50% shows the floor through it. |
| Outline | Draws a colored edge on the tile's own shape, not around the grid. **Outline color**, right under it, picks its color |
| Inner glow | Like the outline, but soft, fading inward. **Glow color**, right under it, picks its color |
| Blur | Softens a monster or object |
| Waver | Moves pixels along a wave, like heat haze |
| Sparkle | Lights a few random pixels. **Sparkle color**, right under it, picks the color they light up in: white unless you change it |
| Color range | Limits hue, saturation, and lightness to some pixels. Pick a range to start from, such as **Reds** or **Greys**, and then change its limits: **Hues from** and **Hues to**, which wrap past red when the second is lower and take every hue when they are equal; **Lightness from** and **to**; and **Colorfulness from** and **to**, where 0% to 20% are the greys. A spectrum under the settings shows the range in full color and the rest dimmed: hues from left to right, bright to dark from top to bottom, and the greys at the right. Click the spectrum to move the nearer hue limit there, or drag across it to set the hues and the lightness at once. A turned color may leave its range, so reds can turn green. Greys have no hue to turn, so for them hue picks a tint and saturation sets how strong it is |
| Animation | Moves between the settings and a second set over time. **Curve** says how it moves, **Period** how long one cycle takes, and **Spread** whether every grid moves together, in a ripple, or at random. |

Use the up and down arrows to pick a setting and the left and right arrows to change it. **Remove the filter** puts back the plain tile. **Esc** goes back to the list, where **s** saves.

A filter changes only the thing you chose, even when other things share its tile: a blue cave orc leaves the other orcs as they were. To do that, the editor gives the thing a tile of its own, in the spare rows past the end of the tile sheet.

With Sixel, every animated tile on screen is sent again at every step, about eight times a second. The preview warns when many are on screen.

## Keys

Fancyband uses Angband's keys. These are the ones you need first:

| Key | What it does |
|---|---|
| Arrow keys or number pad | Move, or attack a monster in the way |
| `.` then a direction | Run |
| `<` and `>` | Go up or down stairs |
| `g` | Pick up |
| `i`, `e`, `\|` | Show your pack, what you wear, your quiver; see [items and spells](#items-and-spells) |
| `w`, `t` | Wear or wield, take off |
| `q`, `r`, `E` | Drink a potion, read a scroll, eat |
| `f`, `v` | Fire a missile, throw |
| `a`, `u`, `z` | Aim a wand, use a staff, zap a rod |
| `b`, `m` | Browse a book, cast a spell |
| `R` | Rest |
| `o`, `c`, `D`, `T` | Open, close, disarm, tunnel |
| `l` | Look around; see [look and target](#look-and-target) |
| `*` | Pick a target |
| `C` | Show your character; see [knowledge, your character, messages, and help](#knowledge-your-character-messages-and-help) |
| `~` | What you know of objects, monsters, and more |
| `/` | What you know of monsters |
| `Ctrl+P` | Show past messages |
| `M` | The whole level; see [the level map](#the-level-map) |
| `Enter` | A menu of every command; see [the command menu](#the-command-menu) |
| `=` | Options, including the roguelike keys that move with `h`, `j`, `k`, `l` |
| `?` | The game's help, with every command |
| `'` | List the names on the screen that have a description; see [read what a name means](#read-what-a-name-means) |
| `F2` | The Tile Editor for this character; see [change the tiles](#change-the-tiles) |
| `Escape` or `F10` | The game menu, which also turns the borg on and off |
| `Ctrl+X` or `Ctrl+C` | Save and quit |

Clicking the map with the mouse walks there.

### The command menu

Press **Enter** for a menu of every command, in the game's groups: items, actions, managing items, information, utility, and hidden commands. Choose a group, then a command. Each command shows its key in the keyset you use, original or roguelike, so the menu also teaches the keys. An entry marked `›` opens a smaller menu, such as the debug commands. Move with the arrow keys, the page keys, or the mouse wheel; choose with **Enter** or a click. **Escape** goes back one menu, and closes the menu from the groups.

## Where Fancyband keeps your files

Everything the game writes goes in `~/.angband/Fancyband`:

| Folder or file | What it holds |
|---|---|
| `save/` | Your characters |
| `scores/` | The high score list |
| `panic/` | Emergency saves, if the game ever crashes |
| `fancyband-tui.json` | The client's settings, such as the character you played last |
| `tiles/` | Your changes to each tile set for every character, from the Tile Editor |
| `symbols.prf` | Your letters and colors for text mode, from the Tile Editor |
| `characters/` | Each character's own tiles and letters, from the Tile Editor |
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
| `limoni-LICENSE`, `limoni-FORK.md` | Limoni, the terminal library in `fancyband`, under Apache 2.0, and what Fancyband changed in it |
| `golang.org-x-sys-LICENSE`, `go-LICENSE` | Go libraries built into `fancyband` |

The source code for this release is in the matching `fancyband-*-src.tar.gz`.
