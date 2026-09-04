package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

type slashCommand struct {
	name  string
	usage string
}

var slashCommandList = []slashCommand{
	{"/exit", "/exit"},
	{"/govern", "/govern"},
	{"/history", "/history"},
	{"/new", "/new"},
	{"/resume", "/resume <id>"},
	{"/retry", "/retry"},
	{"/session", "/session"},
	{"/sessions", "/sessions"},
	{"/tools", "/tools off|summary|verbose"},
	{"/ungovern", "/ungovern"},
}

type slashTrie struct {
	children map[rune]*slashTrie
	matches  []int
}

func newSlashTrie(commands []slashCommand) *slashTrie {
	root := &slashTrie{children: map[rune]*slashTrie{}}
	for i, command := range commands {
		node := root
		for _, char := range command.name {
			next := node.children[char]
			if next == nil {
				next = &slashTrie{children: map[rune]*slashTrie{}}
				node.children[char] = next
			}
			node = next
			node.matches = append(node.matches, i)
		}
	}
	return root
}

func (t *slashTrie) find(prefix string) []slashCommand {
	node := t
	for _, char := range prefix {
		node = node.children[char]
		if node == nil {
			return nil
		}
	}
	commands := make([]slashCommand, 0, len(node.matches))
	for _, index := range node.matches {
		commands = append(commands, slashCommandList[index])
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].name < commands[j].name })
	return commands
}

var chatSlashTrie = newSlashTrie(slashCommandList)

func slashCommandSummary() string {
	values := make([]string, 0, len(slashCommandList))
	for _, command := range slashCommandList {
		values = append(values, command.usage)
	}
	return strings.Join(values, " ")
}

func slashMatches(line string) []slashCommand {
	if !strings.HasPrefix(line, "/") || strings.ContainsAny(line, " \t") {
		return nil
	}
	return chatSlashTrie.find(line)
}

func slashHint(matches []slashCommand) string {
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		values = append(values, match.usage)
	}
	return strings.Join(values, "  ")
}

func fitSlashHint(hint string, width int) string {
	if width <= 4 || len([]rune(hint)) <= width-2 {
		return hint
	}
	runes := []rune(hint)
	return string(runes[:width-5]) + "..."
}

func completeSlash(line string, matches []slashCommand) string {
	if len(matches) == 0 {
		return line
	}
	prefix := matches[0].name
	for _, match := range matches[1:] {
		for !strings.HasPrefix(match.name, prefix) {
			_, size := utf8.DecodeLastRuneInString(prefix)
			prefix = prefix[:len(prefix)-size]
		}
	}
	if len(matches) == 1 && prefix == line && strings.Contains(matches[0].usage, " ") {
		return line + " "
	}
	if len(prefix) > len(line) {
		return prefix
	}
	return line
}

type lineScanner interface {
	Scan() bool
	Text() string
	Err() error
}

type chatInput struct {
	scanner  lineScanner
	in       *os.File
	out      *os.File
	renderer *chatRenderer
	enhanced bool
}

func newChatInput(scanner lineScanner, in *os.File, out io.Writer, renderer *chatRenderer) *chatInput {
	outFile, outputTerminal := out.(*os.File)
	enhanced := in != nil && outputTerminal && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(outFile.Fd())) && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == ""
	return &chatInput{scanner: scanner, in: in, out: outFile, renderer: renderer, enhanced: enhanced}
}

func (i *chatInput) readLine(ctx context.Context, hints bool) (string, error) {
	if !i.enhanced {
		return scanLine(ctx, i.scanner)
	}
	line, err := readSlashLine(ctx, i.in, i.out, i.renderer, hints)
	if err != errTerminalUnavailable {
		return line, err
	}
	i.enhanced = false
	return scanLine(ctx, i.scanner)
}

var errTerminalUnavailable = fmt.Errorf("terminal raw mode unavailable")

func readSlashLine(ctx context.Context, in, out *os.File, renderer *chatRenderer, hints bool) (string, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return "", errTerminalUnavailable
	}
	defer term.Restore(int(in.Fd()), state)
	width, _, _ := term.GetSize(int(out.Fd()))
	if width <= 0 {
		width = 80
	}
	line := ""
	var utf8Bytes []byte
	hadHint := false
	redraw := func() {
		var matches []slashCommand
		if hints {
			matches = slashMatches(line)
		}
		hint := fitSlashHint(slashHint(matches), width)
		renderer.mu.Lock()
		defer renderer.mu.Unlock()
		fmt.Fprint(out, "\r\033[2K", renderer.label("YOU >", colorCyan), " ", safeTerminal(line))
		if hint != "" {
			fmt.Fprint(out, "\n\033[2K  ", safeTerminal(hint), "\033[1A\r")
			if column := len("YOU > ") + len([]rune(line)); column > 0 {
				fmt.Fprintf(out, "\033[%dC", column)
			}
			hadHint = true
		} else if hadHint {
			fmt.Fprint(out, "\n\033[2K\033[1A\r")
			if column := len("YOU > ") + len([]rune(line)); column > 0 {
				fmt.Fprintf(out, "\033[%dC", column)
			}
			hadHint = false
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", ctx.Err()
		}
		var one [1]byte
		if _, err := in.Read(one[:]); err != nil {
			return "", err
		}
		switch one[0] {
		case '\r', '\n':
			if hadHint {
				renderer.mu.Lock()
				fmt.Fprint(out, "\r\033[2K", renderer.label("YOU >", colorCyan), " ", safeTerminal(line), "\n\033[2K\033[1A\r")
				renderer.mu.Unlock()
			}
			fmt.Fprintln(out)
			return line, nil
		case 3:
			return "", io.EOF
		case 4:
			if line == "" {
				return "", io.EOF
			}
		case 8, 127:
			if line != "" {
				_, size := utf8.DecodeLastRuneInString(line)
				line = line[:len(line)-size]
				redraw()
			}
		case '\t':
			if hints {
				line = completeSlash(line, slashMatches(line))
			}
			redraw()
		case 0x1b:
			if err := discardEscapeSequence(in); err != nil {
				return "", err
			}
		default:
			if one[0] >= 0x20 && one[0] != 0x7f && len(line) < 64<<10 {
				utf8Bytes = append(utf8Bytes, one[0])
				if utf8.FullRune(utf8Bytes) {
					r, size := utf8.DecodeRune(utf8Bytes)
					if r != utf8.RuneError || size > 1 {
						line += string(r)
					}
					utf8Bytes = utf8Bytes[size:]
					redraw()
				}
			}
		}
	}
}

func discardEscapeSequence(in io.Reader) error {
	var one [1]byte
	if _, err := io.ReadFull(in, one[:]); err != nil {
		return err
	}
	if one[0] != '[' && one[0] != 'O' {
		return nil
	}
	for {
		if _, err := io.ReadFull(in, one[:]); err != nil {
			return err
		}
		if one[0] >= 0x40 && one[0] <= 0x7e {
			return nil
		}
	}
}
