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
}

// Item is one object in a snapshot or a request's choices.
type Item struct {
	Ref    ItemRef `json:"ref"`
	Name   string  `json:"name"`
	Number int     `json:"number"`
	Weight int     `json:"weight"`
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
	// Screen requests: the screen's name, and which view of it, such as
	// "equip" for the inventory screen.
	Name string `json:"name"`
	View string `json:"view"`
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
