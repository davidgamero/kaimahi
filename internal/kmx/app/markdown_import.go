package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/secretshapes"
	"go.yaml.in/yaml/v3"
)

// ImportMarkdownAgent returns the saved path. New-file publication never replaces
// an existing file; replacing the original requires a separate explicit choice.
func ImportMarkdownAgent(ctx context.Context, path string, in io.Reader, out io.Writer) (string, error) {
	if !strings.HasSuffix(path, ".agent.md") {
		return "", errors.New("import requires a .agent.md file")
	}
	raw, err := mdRead(path, markdownAgentLimit)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) || bytes.ContainsRune(raw, 0) || secretshapes.Match(string(raw)) != nil {
		return "", errors.New("import requires UTF-8 instructions without credential-shaped content")
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", errors.New("add instructions before importing the agent")
	}
	first := strings.TrimSuffix(strings.SplitN(string(raw), "\n", 2)[0], "\r")
	if first == "---" {
		return "", errors.New("agent already has front matter; use validate to inspect it")
	}
	name := strings.TrimSuffix(filepath.Base(path), ".agent.md")
	if !markdownAgentName.MatchString(name) {
		name = "my-agent"
	}
	scanner := bufio.NewScanner(mdImportLineReader{in})
	scanner.Buffer(make([]byte, 1024), 4096)
	ask := func(label, defaultValue string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := fmt.Fprintf(out, "%s [%s]: ", label, defaultValue); err != nil {
			return "", err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		v := strings.TrimSpace(scanner.Text())
		if v == "" {
			v = defaultValue
		}
		return v, nil
	}
	choice, err := ask("No front matter. Import agent? Y/n", "Y")
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(choice, "y") && !strings.EqualFold(choice, "yes") {
		return "", errors.New("agent import declined; file unchanged")
	}
	name, err = ask("Name", name)
	if err != nil {
		return "", err
	}
	description, err := ask("Description", strings.ReplaceAll(strings.ReplaceAll(name, "-", " "), "_", " ")+" agent")
	if err != nil {
		return "", err
	}
	model, err := ask("Model (default uses Copilot selection)", "default")
	if err != nil {
		return "", err
	}
	if model == "default" {
		model = ""
	}
	header := struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Model       string   `yaml:"model,omitempty"`
		Tools       []string `yaml:"tools"`
	}{name, description, model, []string{}}
	front, err := yaml.Marshal(header)
	if err != nil {
		return "", err
	}
	result := append([]byte("---\n"), front...)
	result = append(result, []byte("---\n\n")...)
	result = append(result, raw...)
	if _, err = parseMarkdownAgent(path, result); err != nil {
		return "", err
	}
	if _, err = fmt.Fprintf(out, "\n%s\nTools: none (tools: []). Instructions preserved exactly.\n", front); err != nil {
		return "", err
	}
	choice, err = ask("Save to new file or replace original? new/replace/cancel", "new")
	if err != nil {
		return "", err
	}
	destination := path
	replace := false
	switch strings.ToLower(choice) {
	case "new":
		destination, err = ask("New file", strings.TrimSuffix(path, ".agent.md")+".imported.agent.md")
		if err != nil {
			return "", err
		}
		if !strings.HasSuffix(destination, ".agent.md") {
			return "", errors.New("new file must end in .agent.md")
		}
		if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("new file already exists or is unavailable; choose another name")
		}
	case "replace":
		choice, err = ask("Replace "+safeTerminal(path)+"? y/N", "N")
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(choice, "y") && !strings.EqualFold(choice, "yes") {
			return "", errors.New("replacement cancelled; original unchanged")
		}
		replace = true
	default:
		return "", errors.New("agent import cancelled; original unchanged")
	}
	current, err := mdRead(path, markdownAgentLimit)
	if err != nil || !bytes.Equal(current, raw) {
		return "", errors.New("agent changed while importing; reload and try again")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("agent is no longer a regular file")
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".kmx-import-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return "", err
	}
	if _, err = temp.Write(result); err != nil {
		temp.Close()
		return "", err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return "", err
	}
	if err = temp.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	current, err = mdRead(path, markdownAgentLimit)
	if err != nil || !bytes.Equal(current, raw) {
		return "", errors.New("agent changed before save; original unchanged")
	}
	if replace {
		err = os.Rename(temp.Name(), destination)
	} else {
		err = os.Link(temp.Name(), destination)
	}
	if err != nil {
		return "", err
	}
	_, err = fmt.Fprintln(out, "Imported agent:", safeTerminal(destination))
	return destination, err
}

type mdImportLineReader struct{ io.Reader }

func (r mdImportLineReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.Reader.Read(p[:1])
}
