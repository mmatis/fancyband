// Command netdump prints the frames a fancyband engine sends, as
// readable text. It is a debugging tool for the protocol in
// proto/README.md.
//
// It works three ways:
//
//	netdump FILE                     decode a captured byte stream ("-" for stdin)
//	netdump -connect PATH            talk to "fancyband -mnet -- --listen PATH"
//	netdump -run BINARY [ARGS...]    start "BINARY -mnet ARGS..." itself
//
// When talking to an engine, each line typed on standard input is sent as
// key presses. Plain characters are sent as they are; these tokens send
// special keys: <esc> <enter> <tab> <bs> <up> <down> <left> <right>
// <^X> (any control letter) <kp8> (keypad digit) and <quit>, which sends
// QUIT. The newline at the end of a line is not sent.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"fancyband/internal/grid"
	"fancyband/internal/proto"
	"fancyband/internal/session"
)

func main() {
	connect := flag.String("connect", "", "connect to an engine listening on this Unix socket")
	run := flag.Bool("run", false, "start the engine given as the first argument")
	screen := flag.Bool("screen", false, "print the main term as text after each FRESH instead of every frame")
	size := flag.String("size", "80x24", "main term size to ask for, as WIDTHxHEIGHT")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: netdump [-screen] FILE | -connect PATH | -run BINARY [ARGS...]")
		flag.PrintDefaults()
	}
	flag.Parse()

	var w, h uint16
	if _, err := fmt.Sscanf(*size, "%dx%d", &w, &h); err != nil {
		fatalf("bad -size %q", *size)
	}
	opts := session.Options{
		Terms:  []proto.TermSize{{Index: 0, Width: w, Height: h}},
		Stderr: os.Stderr,
	}

	p := &printer{screenMode: *screen}
	switch {
	case *connect != "":
		s, err := session.Connect(*connect, opts)
		if err != nil {
			fatalf("%v", err)
		}
		talk(s, p)
	case *run:
		if flag.NArg() < 1 {
			flag.Usage()
			os.Exit(2)
		}
		opts.Binary = flag.Arg(0)
		opts.Args = append([]string{"-mnet"}, flag.Args()[1:]...)
		s, err := session.Start(opts)
		if err != nil {
			fatalf("%v", err)
		}
		talk(s, p)
		s.Wait()
	default:
		if flag.NArg() != 1 {
			flag.Usage()
			os.Exit(2)
		}
		var in io.Reader = os.Stdin
		if name := flag.Arg(0); name != "-" {
			f, err := os.Open(name)
			if err != nil {
				fatalf("%v", err)
			}
			defer f.Close()
			in = f
		}
		dump(bufio.NewReader(in), p)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "netdump: "+format+"\n", args...)
	os.Exit(1)
}

// dump prints every frame in a captured stream.
func dump(r io.Reader, p *printer) {
	for {
		f, err := proto.ReadFrame(r)
		if err == io.EOF {
			return
		}
		if err != nil {
			fatalf("%v", err)
		}
		m, err := proto.Decode(f)
		if err != nil {
			fatalf("%v", err)
		}
		p.print(m)
	}
}

// talk prints what the engine sends and sends what the user types.
func talk(s *session.Session, p *printer) {
	p.print(s.Server)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			msgs, err := parseKeys(sc.Text())
			if err != nil {
				fmt.Fprintln(os.Stderr, "netdump:", err)
				continue
			}
			for _, m := range msgs {
				if err := s.Send(m); err != nil {
					return
				}
			}
		}
	}()
	for {
		m, err := s.Recv()
		if err != nil {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, "netdump:", err)
			}
			return
		}
		p.print(m)
		if _, ok := m.(proto.Bye); ok {
			return
		}
	}
}

var namedKeys = map[string]uint32{
	"esc": proto.KeyEscape, "enter": proto.KeyEnter, "tab": proto.KeyTab,
	"bs": proto.KeyBackspace, "up": proto.KeyArrowUp, "down": proto.KeyArrowDown,
	"left": proto.KeyArrowLeft, "right": proto.KeyArrowRight,
}

// parseKeys turns one typed line into KEY and QUIT messages.
func parseKeys(line string) ([]proto.Message, error) {
	var out []proto.Message
	for line != "" {
		if strings.HasPrefix(line, "<") {
			if end := strings.IndexByte(line, '>'); end > 0 {
				tok := line[1:end]
				switch {
				case tok == "quit":
					out = append(out, proto.Quit{})
				case namedKeys[tok] != 0:
					out = append(out, proto.Key{Code: namedKeys[tok]})
				case len(tok) == 2 && tok[0] == '^':
					out = append(out, proto.Key{Code: proto.Ctrl(tok[1])})
				case len(tok) == 3 && strings.HasPrefix(tok, "kp"):
					out = append(out, proto.Key{Code: uint32(tok[2]), Mods: proto.ModKeypad})
				default:
					return nil, fmt.Errorf("unknown key <%s>", tok)
				}
				line = line[end+1:]
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(line)
		out = append(out, proto.Key{Code: uint32(r)})
		line = line[n:]
	}
	return out, nil
}

type printer struct {
	screenMode bool
	set        grid.Set
}

func (p *printer) print(m proto.Message) {
	if p.screenMode {
		if term := p.set.Apply(m); term == 0 {
			fmt.Print("\x1b[H\x1b[2J", p.set.Terms[0].String())
		}
		if bye, ok := m.(proto.Bye); ok {
			fmt.Printf("BYE %q\n", bye.Reason)
		}
		return
	}
	fmt.Println(describe(m))
}

// describe returns one line for a message.
func describe(m proto.Message) string {
	switch m := m.(type) {
	case proto.Hello:
		return fmt.Sprintf("HELLO version=%d name=%q terms=%v caps=%q", m.Version, m.Name, m.Terms, m.Caps)
	case proto.TermOpen:
		return fmt.Sprintf("TERM_OPEN term=%d size=%dx%d name=%q", m.Term, m.Width, m.Height, m.Name)
	case proto.Palette:
		return fmt.Sprintf("PALETTE %d colors", len(m.Colors))
	case proto.Text:
		return fmt.Sprintf("TEXT term=%d x=%d y=%d color=%d bg=%d rev=%v %q",
			m.Term, m.X, m.Y, m.Attr.Color(), m.Attr.Background(), m.Attr.Reversed(), m.Text)
	case proto.Wipe:
		return fmt.Sprintf("WIPE term=%d x=%d y=%d n=%d", m.Term, m.X, m.Y, m.N)
	case proto.Clear:
		return fmt.Sprintf("CLEAR term=%d", m.Term)
	case proto.Cursor:
		return fmt.Sprintf("CURSOR term=%d x=%d y=%d visible=%v big=%v", m.Term, m.X, m.Y, m.Visible(), m.Big())
	case proto.Fresh:
		return fmt.Sprintf("FRESH term=%d", m.Term)
	case proto.Bell:
		return "BELL"
	case proto.Bye:
		return fmt.Sprintf("BYE %q", m.Reason)
	case proto.Key:
		return fmt.Sprintf("KEY code=%#x mods=%#x", m.Code, m.Mods)
	case proto.Mouse:
		return fmt.Sprintf("MOUSE term=%d x=%d y=%d button=%d mods=%#x", m.Term, m.X, m.Y, m.Button, m.Mods)
	case proto.Resize:
		return fmt.Sprintf("RESIZE term=%d size=%dx%d", m.Term, m.Width, m.Height)
	case proto.Quit:
		return "QUIT"
	case proto.Unknown:
		return fmt.Sprintf("%v %d bytes", m.Raw.Type, len(m.Raw.Payload))
	}
	return fmt.Sprintf("%#v", m)
}
