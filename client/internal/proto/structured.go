package proto

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The structured messages carry a JSON object. The types below hold the
// raw JSON; Parse and the typed structs read it. proto/README.md
// describes every field.

// Event reports something that happened in the game.
type Event struct{ JSON []byte }

func (m Event) Frame() Frame { return Frame{TypeEvent, m.JSON} }

// Snapshot describes what the player knows.
type Snapshot struct{ JSON []byte }

func (m Snapshot) Frame() Frame { return Frame{TypeSnapshot, m.JSON} }

// Request is a prompt the client must answer with a Reply.
type Request struct{ JSON []byte }

func (m Request) Frame() Frame { return Frame{TypeRequest, m.JSON} }

// Command asks the engine to run a game command.
type Command struct{ JSON []byte }

func (m Command) Frame() Frame { return Frame{TypeCommand, m.JSON} }

// Reply answers a Request.
type Reply struct{ JSON []byte }

func (m Reply) Frame() Frame { return Frame{TypeReply, m.JSON} }

func clone(b []byte) []byte { return append([]byte(nil), b...) }

// EventData is an EVENT's JSON. Fields an event does not carry are zero.
type EventData struct {
	Event   string `json:"event"`
	Context string `json:"context,omitempty"`
	// COMMAND results
	ID    int    `json:"id,omitempty"`
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
	// HP, MANA
	HP  int `json:"hp,omitempty"`
	MHP int `json:"mhp,omitempty"`
	SP  int `json:"sp,omitempty"`
	MSP int `json:"msp,omitempty"`
	// MESSAGE
	Text string `json:"text,omitempty"`
	Type int    `json:"type,omitempty"`
	// PLAYERMOVED
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`
	// DUNGEONLEVEL
	Depth int `json:"depth,omitempty"`
	// BORG
	On bool `json:"on,omitempty"`
	// MOVES: each creature's path, and whether the game is busy running
	// or resting
	Moves []MoveData `json:"moves,omitempty"`
	Busy  bool       `json:"busy,omitempty"`
	// SCROLL: how far the map's contents move on the main term, in
	// cells, and the cells that show the map
	Shift [2]int   `json:"shift,omitempty"`
	Map   *BoxData `json:"map,omitempty"`
	// SAVEFILES: the saves, and the savefile this engine uses, which may
	// not exist yet
	Saves   []SaveData `json:"saves,omitempty"`
	Current string     `json:"current,omitempty"`
	// OPTIONS: whether a character has loaded, every option, and the
	// number settings
	Loaded   bool           `json:"loaded,omitempty"`
	Options  []OptionData   `json:"options,omitempty"`
	Numbers  map[string]int `json:"numbers,omitempty"`
	Graphics *GraphicsData  `json:"graphics,omitempty"`
	// VISUALS: the tile set's name for its file of changes, the names of
	// the slots terrain and traps have (kinds of light) and effects have
	// (bolt directions), and every entry that can take a tile. File is
	// the path of the file of changes; a Brief answer stops there.
	// VisualLayer is the layer the tiles are given up to, and File that
	// layer's file of changes. VisualFiles names every layer's file by
	// layer: defaults, global, and character once there is one.
	VisualSet   string            `json:"set,omitempty"`
	VisualLayer string            `json:"layer,omitempty"`
	VisualFiles map[string]string `json:"files,omitempty"`
	File        string            `json:"file,omitempty"`
	Brief       bool              `json:"brief,omitempty"`
	Lights      []string          `json:"lights,omitempty"`
	Directions  []string          `json:"directions,omitempty"`
	// SymbolColors names the colors a symbol can take, by number.
	SymbolColors []string `json:"colors,omitempty"`
	// PlayerEntry is the player entry for the character's race and
	// class, or 0 before there is a character.
	PlayerEntry int          `json:"player_entry,omitempty"`
	Entries     []VisualData `json:"entries,omitempty"`
	// RECALL: what is described, and its description, or nil when there
	// is nothing to describe
	Title       *string    `json:"title,omitempty"`
	Description StyledText `json:"description,omitempty"`
	// INVENTORY: what the character carries, each item with its actions;
	// the name of every equipment slot; and the weight carried and what
	// is left before it slows the character, in tenths of a pound
	Inventory []Item   `json:"inventory,omitempty"`
	Slots     []string `json:"slots,omitempty"`
	Equipment []Item   `json:"equipment,omitempty"`
	Quiver    []Item   `json:"quiver,omitempty"`
	Burden    int      `json:"burden,omitempty"`
	Remaining int      `json:"remaining,omitempty"`
	// SPELLS: a book's spells, and what its realm calls a spell
	Spells []SpellData `json:"spells,omitempty"`
	Noun   string      `json:"noun,omitempty"`
	// STORE: the store the character is in, or nil outside one
	Store *StoreInfo `json:"store,omitempty"`
	// BIRTH: the character being made, or nil outside character creation
	Birth *BirthInfo `json:"birth,omitempty"`
	// LOOK: the interesting grids, nearest first, and the grid the
	// player targets, if any
	Things []LookThing `json:"things,omitempty"`
	Target *TargetData `json:"target,omitempty"`
	// MONSTER_LIST: the monster race each row of the monster list
	// subwindow shows, or nil for a row that shows none
	Rows []*int `json:"rows,omitempty"`
	// KNOWLEDGE: the sections of the knowledge menu, or one section's
	// entries
	Sections []KnowledgeSection `json:"sections,omitempty"`
	Section  string             `json:"section,omitempty"`
	List     []KnowledgeEntry   `json:"list,omitempty"`
	// CHARACTER: the character sheet, or nil without a character
	Character *CharacterSheet `json:"character,omitempty"`
	// MESSAGES: the message history, oldest first
	Messages []MessageData `json:"messages,omitempty"`
	// HELP: a help file (in File), its text or nil when there is no such
	// file, the files its keys open, and the lines its tags mark
	Page  StyledText `json:"page,omitempty"`
	Links []HelpLink `json:"links,omitempty"`
	Tags  []HelpTag  `json:"tags,omitempty"`
	// MAP: the whole level as the player remembers it
	Level *LevelMap `json:"level_map,omitempty"`
	// TOMB: what the tombstone shows, or nil without a character
	Tomb *TombData `json:"tomb,omitempty"`
	// COMMANDS: the command menu Enter opens
	Commands *CommandMenu `json:"commands,omitempty"`
	// COMPARE: the equipment comparison, or nil without a character
	Compare *CompareData `json:"compare,omitempty"`
}

// CompareData is a COMPARE answer: the equipment comparison, with every
// item from every source.
type CompareData struct {
	// Props are the property columns, in the game's order.
	Props []CompareProp `json:"props"`
	// Player is the character with their equipment.
	Player CompareRow `json:"player"`
	// Items are in the game's order.
	Items []CompareItem `json:"items"`
}

// CompareRow is the character's row in the comparison: a letter, a
// color, and a value per property.
type CompareRow struct {
	Text   string `json:"text"`
	Colors []int  `json:"colors"`
	Values []int  `json:"values"`
}

// CompareProp is one property column: its Category ("resistances",
// "abilities", "hindrances", "modifiers", or "stat_modifiers"), its
// two-letter Code, and its full Name.
type CompareProp struct {
	Category string `json:"category"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}

// CompareItem is one item in the comparison: the object, where it is
// (Source: 'e' worn, 'p' pack, 'f' floor, 'h' home, 's' store), its rough
// Quality, its equipment Slot, its cells, a letter and a color per
// property, and its Values, a number per property.
type CompareItem struct {
	Item
	Source  string `json:"source"`
	Quality string `json:"quality"`
	Slot    int    `json:"slot"`
	Text    string `json:"text"`
	Colors  []int  `json:"colors"`
	Values  []int  `json:"values"`
}

// TombData is a TOMB answer: what the game's tombstone shows.
type TombData struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Class    string `json:"class"`
	Level    int    `json:"level"`
	Exp      int    `json:"exp"`
	Gold     int    `json:"gold"`
	Depth    int    `json:"depth"`
	DiedFrom string `json:"died_from"`
	Dead     bool   `json:"dead"`
	Retired  bool   `json:"retired"`
	Winner   bool   `json:"winner"`
	Date     string `json:"date"`
}

// CommandMenu is a COMMANDS answer: the game's command menu.
type CommandMenu struct {
	// Keyset is "original" or "roguelike".
	Keyset string `json:"keyset"`
	// Lists are the game's command lists. Those at level 0 are the
	// groups the menu opens on; the rest are submenus.
	Lists []CommandList `json:"lists"`
}

// CommandList is one list of commands in a CommandMenu.
type CommandList struct {
	Name     string         `json:"name"`
	Level    int            `json:"level"`
	Commands []CommandEntry `json:"commands"`
}

// CommandEntry is one entry in a CommandList: a command, with its key in
// the keyset in use if it has one, or, when Submenu is set, the index of
// the list it opens.
type CommandEntry struct {
	Name    string `json:"name"`
	Key     string `json:"key,omitempty"`
	Submenu *int   `json:"submenu,omitempty"`
}

// LevelMap is a MAP answer: the level, grid by grid, as the map view
// draws it.
type LevelMap struct {
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Depth  int        `json:"depth"`
	Player TargetData `json:"player"`
	// Rows holds each row's grids: letters and colors in text mode,
	// tiles with a tile set.
	Rows []LevelRow `json:"rows"`
	// Marks are the stairs and shop entrances the player knows.
	Marks []LevelMark `json:"marks"`
}

// LevelRow is one row of a LevelMap. Text has one letter per grid and
// Colors a palette index per grid; Tiles has four numbers per grid, as
// a PictCell holds them.
type LevelRow struct {
	Text   string `json:"text,omitempty"`
	Colors []int  `json:"colors,omitempty"`
	Tiles  []int  `json:"tiles,omitempty"`
}

// LevelMark is a grid the level map marks: "up" or "down" stairs, or a
// "shop" entrance with the shop's Name.
type LevelMark struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

// KnowledgeSection is one section of the knowledge menu: "objects",
// "monsters", "store:N", "history", and so on. Known says it has
// anything in it yet.
type KnowledgeSection struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Known bool   `json:"known"`
}

// KnowledgeEntry is one entry of a knowledge section.
type KnowledgeEntry struct {
	Group string `json:"group"`
	// ID is what RECALL takes with the section's name.
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Note is short text such as kills, an inscription, or a price.
	Note string `json:"note,omitempty"`
	// Dim marks an entry not fully known yet.
	Dim bool `json:"dim,omitempty"`
	// Glyph is how the entry looks, if it has a letter or tile.
	Glyph
	// Visual is the entry the Tile Editor changes for it, if any.
	Visual *VisualRef `json:"visual,omitempty"`
	// History entries: when, where, and at what level.
	Turn  int `json:"turn,omitempty"`
	Depth int `json:"depth,omitempty"`
	Level int `json:"level,omitempty"`
	// Scores: the rest of the entry, and the place the character would
	// take now.
	Lines   []string `json:"lines,omitempty"`
	Current bool     `json:"current,omitempty"`
}

// VisualRef names an entry of VISUALS.
type VisualRef struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

// CharacterSheet is a CHARACTER answer.
type CharacterSheet struct {
	Name      string `json:"name"`
	ForceName bool   `json:"force_name"`
	// Panels are the sheet's panels in the game's order: name and class,
	// age and turns, experience and gold, combat, and skills.
	Panels  [][]PanelLine `json:"panels"`
	Stats   []SheetStat   `json:"stats"`
	History string        `json:"history"`
	// Abilities is the table of resistances and abilities by equipment
	// slot, one row each, as the game draws it; AbilitiesHead its two
	// heading rows, the equipment and the slots' letters; and SlotColumn
	// the column the slots start at.
	Abilities     StyledText `json:"abilities"`
	AbilitiesHead StyledText `json:"abilities_head"`
	SlotColumn    int        `json:"slot_column"`
	// Equippy is the equipment, slot by slot, nil for an empty slot.
	Equippy []*Glyph `json:"equippy"`
	// Player is the character's letter and tile, as the map shows them.
	Player *Glyph `json:"player,omitempty"`
}

// PanelLine is one line of a character sheet panel; a blank line has no
// label.
type PanelLine struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value,omitempty"`
	Color int    `json:"color,omitempty"`
}

// SheetStat is one row of the character sheet's stats table.
type SheetStat struct {
	Name    string `json:"name"`
	Drained bool   `json:"drained,omitempty"`
	Maxed   bool   `json:"maxed,omitempty"`
	Self    string `json:"self"`
	Race    int    `json:"race"`
	Class   int    `json:"class"`
	Equip   int    `json:"equip"`
	Best    string `json:"best"`
	Current string `json:"current,omitempty"`
}

// MessageData is one message of the history.
type MessageData struct {
	Text  string `json:"text"`
	Count int    `json:"count,omitempty"`
	Type  int    `json:"type"`
	Kind  string `json:"kind,omitempty"`
	Color int    `json:"color"`
}

// HelpLink is a key in a help file and the file it opens.
type HelpLink struct {
	Key  string `json:"key"`
	File string `json:"file"`
}

// HelpTag is a line of a help file that a link can jump to.
type HelpTag struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// LookThing is one grid LOOK lists, as look mode describes it.
type LookThing struct {
	X int `json:"x"`
	Y int `json:"y"`
	// Cell is where the grid's tile starts on the main term, or nil
	// when the map does not show it.
	Cell *[2]int `json:"cell"`
	// Free marks a grid the player picked that look mode passes over.
	Free bool `json:"free"`
	// Kind is the first thing look mode shows there: "player",
	// "monster", "trap", "object", "pile", "terrain", or "strange" while
	// the character hallucinates.
	Kind string `json:"kind"`
	// Name is that thing, such as "a Jackal", Text the sentence look
	// mode prints, and Where how far it is, such as "3 N, 2 E".
	Name  string `json:"name"`
	Text  string `json:"text"`
	Where string `json:"where"`
	// Char, Color, and Tile are how the grid looks on the map.
	Glyph
	// A monster's race id for RECALL, its index for SET_TARGET, its
	// health as look mode gives it, and whether it can be targeted.
	Monster    int    `json:"monster"`
	Midx       int    `json:"midx"`
	Health     string `json:"health"`
	Targetable bool   `json:"targetable"`
	// The trap kind and terrain feature there, for RECALL.
	Trap    *int `json:"trap"`
	Feature *int `json:"feature"`
	// Objects are the objects the player knows of there.
	Objects []LookObject `json:"objects"`
}

// LookObject is an object LOOK lists on a grid.
type LookObject struct {
	Name string `json:"name"`
	Glyph
}

// TargetData is the grid the player targets, and the monster there if
// the target is a monster.
type TargetData struct {
	X       int `json:"x"`
	Y       int `json:"y"`
	Monster int `json:"monster"`
}

// StoreInfo is a STORE answer: the store, what it sells, and what it
// would buy from the character.
type StoreInfo struct {
	Name string `json:"name"`
	// Owner is the shopkeeper, and Purse the most the owner pays for one
	// object. The home has neither.
	Owner string `json:"owner"`
	Purse int    `json:"purse"`
	Home  bool   `json:"home"`
	Gold  int    `json:"gold"`
	// NoSelling says the store pays nothing for what it takes.
	NoSelling bool `json:"no_selling"`
	// Stock is what the store sells, Gear what the character carries:
	// the pack, then the quiver, then the equipment.
	Stock []StoreItem `json:"stock"`
	Gear  []StoreItem `json:"gear"`
}

// StoreItem is an object in a STORE answer.
type StoreItem struct {
	Item
	// Max is the most of it the character can buy or sell now, or 0, with
	// Why saying why not.
	Max int    `json:"max"`
	Why string `json:"why,omitempty"`
	// Have is how many of a stock object the pack already holds.
	Have int `json:"have,omitempty"`
	// Price is what one costs in the store. Prices[n-1] is what n cost,
	// or fetch when sold; the home has no prices.
	Price  int   `json:"price,omitempty"`
	Prices []int `json:"prices,omitempty"`
}

// BirthInfo is a BIRTH answer: the character being made.
type BirthInfo struct {
	// Quickstart says the last character can be made again as it was,
	// and ForceName that the name cannot change.
	Quickstart bool          `json:"quickstart"`
	ForceName  bool          `json:"force_name"`
	Races      []BirthChoice `json:"races"`
	Classes    []BirthChoice `json:"classes"`
	// Race and Class are the chosen ones, by index.
	Race  int         `json:"race"`
	Class int         `json:"class"`
	Stats []BirthStat `json:"stats"`
	// Rolled says the roller made the stats; otherwise Points are left to
	// buy them with.
	Rolled  bool   `json:"rolled"`
	Points  int    `json:"points"`
	HP      int    `json:"hp"`
	Gold    int    `json:"gold"`
	Age     int    `json:"age"`
	Height  int    `json:"height"`
	Weight  int    `json:"weight"`
	Name    string `json:"name"`
	History string `json:"history"`
	// NameUsed says a savefile already has the name asked about.
	NameUsed bool `json:"name_used"`
}

// BirthChoice is a race or class, with what it is good at.
type BirthChoice struct {
	Name string     `json:"name"`
	Help StyledText `json:"help"`
}

// BirthStat is one stat of the character being made: its own value, the
// race's and class's changes, and the value with them. Point-based stats
// also have the points spent on it, and Cost, what the next point costs,
// below 18.
type BirthStat struct {
	Name  string `json:"name"`
	Self  int    `json:"self"`
	Race  int    `json:"race"`
	Class int    `json:"class"`
	Best  int    `json:"best"`
	Spent int    `json:"spent"`
	Cost  *int   `json:"cost,omitempty"`
}

// BoxData is a rectangle of cells on a term.
type BoxData struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// MoveData is one creature's moves in a MOVES event: the cells on the
// main term where its tile started, then each grid it stepped to, as
// [column, row] pairs.
type MoveData struct {
	Path [][2]int `json:"path"`
}

// SaveData is one saved character in a SAVEFILES answer.
type SaveData struct {
	// Name is what the engine's -u option takes.
	Name        string `json:"name"`
	Description string `json:"description"`
	Dead        bool   `json:"dead"`
	// Race and Class name a living character's race and class.
	Race  string `json:"race,omitempty"`
	Class string `json:"class,omitempty"`
	// Current marks the savefile the answering engine uses.
	Current bool `json:"current"`
}

// OptionData is one option in an OPTIONS answer.
type OptionData struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Page is interface, birth, cheat, or score.
	Page     string `json:"page"`
	Value    bool   `json:"value"`
	Editable bool   `json:"editable"`
}

// VisualData is one entry in a VISUALS answer: a monster, object kind,
// flavor, terrain feature, trap, or spell effect.
type VisualData struct {
	// Kind is player, monster, object, flavor, terrain, trap, or effect.
	Kind string `json:"kind"`
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Char and Color are its letter and color without tiles. OwnSymbol,
	// when the layer asked for changed them, holds the layers' below;
	// SymbolFrom otherwise names the lower layer that changed them.
	Char       string  `json:"char"`
	Color      int     `json:"color"`
	OwnSymbol  *Symbol `json:"own_symbol,omitempty"`
	SymbolFrom string  `json:"symbol_from,omitempty"`
	// Group heads a run of entries: a player entry's class.
	Group string `json:"group,omitempty"`
	// You marks the player entry for the character alone.
	You bool `json:"you,omitempty"`
	// Flavored is set for an object kind the map shows with its
	// flavor's tile instead of its own.
	Flavored bool `json:"flavored,omitempty"`
	// Tiles is its tile in each slot, as [row, column] on the sheet, or
	// nil for a letter: one slot, or one for each of Lights or
	// Directions. Own, when the layer asked for changed a tile, holds
	// the tiles the layers below give, which a reset gives back. From
	// otherwise names the lower layer that gives its tiles, and Above a
	// higher layer that changes them, which the map shows instead.
	Tiles []*[2]int `json:"tiles"`
	Own   []*[2]int `json:"own,omitempty"`
	From  string    `json:"from,omitempty"`
	Above string    `json:"above,omitempty"`
}

// Symbol is a letter and its color, a palette index.
type Symbol struct {
	Char  string `json:"char"`
	Color int    `json:"color"`
}

// GraphicsData is the tile set in an OPTIONS answer, and the sets the
// engine has.
type GraphicsData struct {
	Set   int `json:"set"`
	Scale int `json:"scale"`
	Sets  []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"sets"`
}

// Option returns the option named name, or false.
func (d EventData) Option(name string) (OptionData, bool) {
	for _, o := range d.Options {
		if o.Name == name {
			return o, true
		}
	}
	return OptionData{}, false
}

// Parse decodes the event's JSON.
func (m Event) Parse() (EventData, error) {
	var d EventData
	err := json.Unmarshal(m.JSON, &d)
	return d, err
}

// ItemRef names an object by where it is. Exactly one field is set.
type ItemRef struct {
	Inven  *int `json:"inven,omitempty"`
	Equip  *int `json:"equip,omitempty"`
	Quiver *int `json:"quiver,omitempty"`
	Floor  *int `json:"floor,omitempty"`
	Store  *int `json:"store,omitempty"`
	// Compare is an item COMPARE lists, which only RECALL takes.
	Compare *int `json:"compare,omitempty"`
}

// Item is an object in a snapshot, a query's answer, or a prompt's
// choices.
type Item struct {
	Ref    ItemRef `json:"ref"`
	Name   string  `json:"name"`
	Number int     `json:"number"`
	// Weight is one object's, in tenths of a pound.
	Weight int `json:"weight"`
	// Char and Color are its letter, and Tile its tile when the map uses
	// tiles.
	Char  string    `json:"char"`
	Color int       `json:"color"`
	Tile  *TileSpot `json:"tile"`
	// Tags are the digits its inscriptions give it, in an item prompt.
	Tags string `json:"tags,omitempty"`
	// Actions are what can be done with it, in INVENTORY.
	Actions []Action `json:"actions,omitempty"`
}

// Action is one thing the game's menu for an object offers to do with
// it.
type Action struct {
	Label string `json:"label"`
	// Cmd is the command, and Key the key the game's menu gives it.
	Cmd string `json:"cmd"`
	Key string `json:"key"`
	// Valid says it can be done now.
	Valid bool `json:"valid"`
	// Arg names the command's argument for the object, when it is not
	// "item", and Quantity is a quantity to send with it.
	Arg      string `json:"arg,omitempty"`
	Quantity int    `json:"quantity,omitempty"`
}

// SpellData is one spell, as the game's spell menu shows it.
type SpellData struct {
	// Value is what a spell prompt's reply gives back.
	Value int    `json:"value"`
	Name  string `json:"name"`
	Level int    `json:"level"`
	Mana  int    `json:"mana"`
	Fail  int    `json:"fail"`
	// Comment is such as "untried" or the spell's damage, and Color the
	// palette color the game's menu draws the spell in.
	Comment string `json:"comment"`
	Color   int    `json:"color"`
	// Usable says the prompt takes it, or in SPELLS that it can be cast
	// now.
	Usable bool `json:"usable"`
}

// PlayerData is the character in a snapshot.
type PlayerData struct {
	Name     string         `json:"name"`
	Race     string         `json:"race"`
	Class    string         `json:"class"`
	Level    int            `json:"level"`
	Exp      int            `json:"exp"`
	MaxExp   int            `json:"max_exp"`
	HP       int            `json:"hp"`
	MHP      int            `json:"mhp"`
	SP       int            `json:"sp"`
	MSP      int            `json:"msp"`
	Gold     int            `json:"gold"`
	Depth    int            `json:"depth"`
	MaxDepth int            `json:"max_depth"`
	X        int            `json:"x"`
	Y        int            `json:"y"`
	Speed    int            `json:"speed"`
	AC       int            `json:"ac"`
	Food     int            `json:"food"`
	Dead     bool           `json:"dead"`
	Stats    map[string]int `json:"stats"`
}

// MonsterData is one visible monster.
type MonsterData struct {
	Name   string `json:"name"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Health int    `json:"health"`
	Asleep bool   `json:"asleep"`
}

// MapData is the map as the player knows it, one string per row. See
// proto/README.md for what each character means.
type MapData struct {
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Rows   []string `json:"rows"`
}

// At returns the map character at x, y, or ' ' outside the map.
func (m *MapData) At(x, y int) byte {
	if m == nil || y < 0 || y >= len(m.Rows) || x < 0 || x >= len(m.Rows[y]) {
		return ' '
	}
	return m.Rows[y][x]
}

// Find returns the first grid holding c, scanning row by row.
func (m *MapData) Find(c byte) (x, y int, ok bool) {
	if m == nil {
		return 0, 0, false
	}
	for y, row := range m.Rows {
		for x := 0; x < len(row); x++ {
			if row[x] == c {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// BirthData lists the races and classes by index, during birth.
type BirthData struct {
	Races   []string `json:"races"`
	Classes []string `json:"classes"`
}

// SnapshotData is a SNAPSHOT's JSON.
type SnapshotData struct {
	Turn      int           `json:"turn"`
	Context   string        `json:"context"`
	Birth     *BirthData    `json:"birth"`
	Player    *PlayerData   `json:"player"`
	Monsters  []MonsterData `json:"monsters"`
	Inventory []Item        `json:"inventory"`
	Equipment []Item        `json:"equipment"`
	Quiver    []Item        `json:"quiver"`
	Floor     []Item        `json:"floor"`
	Map       *MapData      `json:"map"`
	Status    *StatusData   `json:"status"`
}

// StatusData is the status part of a snapshot: what the game's sidebar
// and status line show. Colors are palette indexes.
type StatusData struct {
	Name     string `json:"name"`
	Race     string `json:"race"`
	Class    string `json:"class"`
	Title    string `json:"title"`
	Level    int    `json:"level"`
	MaxLevel int    `json:"max_level"`
	Exp      int    `json:"exp"`
	MaxExp   int    `json:"max_exp"`
	// NextExp is the experience still needed for the next level, or -1
	// at the top level.
	NextExp int `json:"next_exp"`
	Gold    int `json:"gold"`
	// Equippy holds a letter for each equipment slot, or nil for an
	// empty slot.
	Equippy []*Glyph     `json:"equippy"`
	Stats   []StatStatus `json:"stats"`
	AC      int          `json:"ac"`
	HP      int          `json:"hp"`
	MHP     int          `json:"mhp"`
	HPColor int          `json:"hp_color"`
	// Magic says the character has spell points to show.
	Magic   bool `json:"magic"`
	SP      int  `json:"sp"`
	MSP     int  `json:"msp"`
	SPColor int  `json:"sp_color"`
	// Health is the tracked monster's health, or nil.
	Health     *HealthStatus `json:"health"`
	Speed      int           `json:"speed"`
	SpeedText  string        `json:"speed_text"`
	SpeedColor int           `json:"speed_color"`
	Depth      int           `json:"depth"`
	DepthText  string        `json:"depth_text"`
	// Line is the status line, one span per item.
	Line []Span `json:"line"`
}

// Glyph is a letter in a color, and with tiles the tile that stands for
// it.
type Glyph struct {
	Char  string    `json:"char"`
	Color int       `json:"color"`
	Tile  *TileSpot `json:"tile"`
}

// TileSpot is a tile's place on the sheet, numbered as in PICT.
type TileSpot struct {
	Row uint8 `json:"row"`
	Col uint8 `json:"col"`
}

// StatStatus is one stat as the sidebar shows it.
type StatStatus struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
	// Drained says the stat is below its maximum; Maxed that the maximum
	// is the highest a stat can go.
	Drained bool `json:"drained"`
	Maxed   bool `json:"maxed"`
}

// HealthStatus is the tracked monster's health bar.
type HealthStatus struct {
	Color int `json:"color"`
	// Percent is the health left, or -1 when the player cannot tell.
	Percent int `json:"percent"`
	// Name is the monster's, when the player can see it.
	Name string `json:"name,omitempty"`
}

// Has reports whether the status line shows an item that starts with
// text, such as "Poisoned" or "Fast".
func (d *StatusData) Has(text string) bool {
	for _, sp := range d.Line {
		if strings.HasPrefix(sp.Text, text) {
			return true
		}
	}
	return false
}

// Parse decodes the snapshot's JSON.
func (m Snapshot) Parse() (SnapshotData, error) {
	var d SnapshotData
	err := json.Unmarshal(m.JSON, &d)
	return d, err
}

// RequestData is a REQUEST's JSON.
type RequestData struct {
	ID        int             `json:"id"`
	Prompt    string          `json:"prompt"`
	Text      string          `json:"text"`
	Default   string          `json:"default"`
	Max       int             `json:"max"`
	Options   string          `json:"options"`
	AllowNone bool            `json:"allow_none"`
	Verb      string          `json:"verb"`
	Choices   json.RawMessage `json:"choices"`
	// Spell prompts: what the realm calls a spell, and the book's name
	// for spell_from_book.
	Noun string `json:"noun"`
	Book string `json:"book"`
	// Screen requests: the screen's name, and which view of it, such as
	// "equip" for the inventory screen.
	Name string `json:"name"`
	View string `json:"view"`
	// Look and target screens: "look" or "target", a grid the player
	// picked to start on, the target the player had, and whether the
	// screen may travel with PATHFIND.
	Mode     string      `json:"mode"`
	Grid     *TargetData `json:"grid"`
	Target   *TargetData `json:"target"`
	Pathfind bool        `json:"pathfind"`
}

// Items decodes an item prompt's choices.
func (d RequestData) Items() ([]Item, error) {
	var items []Item
	err := json.Unmarshal(d.Choices, &items)
	return items, err
}

// SpellChoices decodes a spell prompt's choices.
func (d RequestData) SpellChoices() ([]SpellData, error) {
	var spells []SpellData
	err := json.Unmarshal(d.Choices, &spells)
	return spells, err
}

// Map returns the reference in its JSON shape, such as {"inven": 2}.
func (r ItemRef) Map() map[string]int {
	out := map[string]int{}
	for k, v := range map[string]*int{"inven": r.Inven, "equip": r.Equip, "quiver": r.Quiver, "floor": r.Floor, "store": r.Store} {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}

// Parse decodes the request's JSON.
func (m Request) Parse() (RequestData, error) {
	var d RequestData
	err := json.Unmarshal(m.JSON, &d)
	return d, err
}

// NewCommand builds a COMMAND. args may be nil.
func NewCommand(id int, name string, args map[string]any) Command {
	body := map[string]any{"id": id, "cmd": name}
	if args != nil {
		body["args"] = args
	}
	return Command{JSON: mustJSON(body)}
}

// NewReply builds a REPLY to request id. fields are the answer, such as
// {"value": 6}; nil cancels the prompt.
func NewReply(id int, fields map[string]any) Reply {
	body := map[string]any{"id": id}
	if fields == nil {
		body["cancel"] = true
	}
	for k, v := range fields {
		body[k] = v
	}
	return Reply{JSON: mustJSON(body)}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("proto: %v", err))
	}
	return b
}

// Span is a run of text in one color, a palette index as in TEXT.
type Span struct {
	Text  string `json:"text"`
	Color int    `json:"color"`
}

// StyledText is the game's colored text: a list of lines, each a list of
// spans. Only the game's own line breaks split lines; a client wraps them
// to fit.
type StyledText [][]Span

// PlainText makes styled text in one color, one line per line of text.
func PlainText(color int, text string) StyledText {
	var st StyledText
	for _, line := range strings.Split(text, "\n") {
		st = append(st, []Span{{Text: line, Color: color}})
	}
	return st
}
