package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/cliui"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"golang.org/x/sys/unix"
)

// AgentInstance describes an execution, never a merely discovered source file.
// The process-held lease is authoritative for liveness; PID is informational.
type AgentInstance struct {
	SchemaVersion   int        `json:"schemaVersion"`
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Environment     string     `json:"environment"`
	Source          string     `json:"source"`
	Version         string     `json:"version"`
	PreviousVersion string     `json:"previousVersion,omitempty"`
	Generation      int        `json:"generation"`
	State           string     `json:"state"`
	Reason          string     `json:"reason,omitempty"`
	PID             int        `json:"pid"`
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	Started         time.Time  `json:"started"`
	Finished        *time.Time `json:"finished,omitempty"`
	Socket          string     `json:"socket,omitempty"`
	Manager         string     `json:"manager,omitempty"`
}
type agentLease struct {
	file   *os.File
	path   string
	record AgentInstance
}

var agentVersionDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func agentInstanceID(source string) string {
	return "md-" + strings.TrimPrefix(mdHash([]byte(source)), "sha256:")
}
func agentStateRoot() (string, error) {
	root, err := config.StateDir()
	return filepath.Join(root, "agents"), err
}

// CacheMarkdownVersion stores exact source bytes once. Existing entries are
// verified rather than rewritten, so all invocations agree on a version's bytes.
func CacheMarkdownVersion(a *MarkdownAgent) (*MarkdownAgent, error) {
	if !agentVersionDigest.MatchString(a.Revision) || mdHash(a.Source) != a.Revision {
		return nil, errors.New("agent snapshot digest mismatch")
	}
	root, err := agentStateRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "versions", strings.TrimPrefix(a.Revision, "sha256:"))
	if err = mdPrivateDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "agent.md")
	if old, e := mdRead(path, markdownAgentLimit); e == nil {
		if !bytes.Equal(old, a.Source) {
			return nil, errors.New("cached agent version is corrupt")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	} else {
		temp, e := os.CreateTemp(dir, ".version-*")
		if e != nil {
			return nil, e
		}
		defer os.Remove(temp.Name())
		if _, e = temp.Write(a.Source); e != nil {
			temp.Close()
			return nil, e
		}
		if e = temp.Sync(); e != nil {
			temp.Close()
			return nil, e
		}
		if e = temp.Close(); e != nil {
			return nil, e
		}
		if e = os.Link(temp.Name(), path); e != nil && !errors.Is(e, os.ErrExist) {
			return nil, e
		}
		old, e := mdRead(path, markdownAgentLimit)
		if e != nil || !bytes.Equal(old, a.Source) {
			return nil, errors.New("cached version verification failed")
		}
	}
	// Parse the actual cache bytes for execution, not a mutable source pathname.
	raw, err := mdRead(path, markdownAgentLimit)
	if err != nil {
		return nil, err
	}
	frozen, err := parseMarkdownAgent(a.Path, raw)
	if err != nil {
		return nil, err
	}
	frozen.TestsPath, frozen.TestsRevision, frozen.PairRevision = a.TestsPath, a.TestsRevision, a.PairRevision
	return frozen, nil
}

func openAgentLease(path string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		f.Close()
		return nil, errors.New("unsafe agent instance lease")
	}
	return f, nil
}
func beginAgentInstance(a *MarkdownAgent, model string) (*agentLease, *MarkdownAgent, error) {
	source, err := filepath.Abs(a.Path)
	if err != nil {
		return nil, nil, err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return nil, nil, err
	}
	root, err := agentStateRoot()
	if err != nil {
		return nil, nil, err
	}
	id := agentInstanceID(source)
	dir := filepath.Join(root, "instances", id)
	if err = mdPrivateDir(dir); err != nil {
		return nil, nil, err
	}
	f, err := openAgentLease(filepath.Join(dir, "active.lock"), true)
	if err != nil {
		return nil, nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("agent %s is already running; stop its foreground session before running an updated version", a.Name)
	}
	failed := true
	defer func() {
		if failed {
			f.Close()
		}
	}()
	recordPath := filepath.Join(dir, "instance.json")
	var old AgentInstance
	if e := mdStateRead(recordPath, &old); e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, nil, e
	}
	if old.ID != "" && (old.ID != id || old.SchemaVersion != 1 || old.Source != source) {
		return nil, nil, errors.New("invalid agent instance record")
	}
	if err = a.CheckCurrent(); err != nil {
		return nil, nil, err
	}
	frozen, err := CacheMarkdownVersion(a)
	if err != nil {
		return nil, nil, err
	}
	frozen.Model = model
	r := AgentInstance{SchemaVersion: 1, ID: id, Name: a.Name, Type: "md", Source: source, Version: a.Revision, PreviousVersion: old.Version, Generation: old.Generation + 1, State: "running", PID: os.Getpid(), Provider: "copilot", Model: model, Started: time.Now().UTC()}
	if old.Version == r.Version {
		r.PreviousVersion = old.PreviousVersion
	}
	lease := &agentLease{file: f, path: recordPath, record: r}
	if err = lease.save(); err != nil {
		return nil, nil, err
	}
	failed = false
	return lease, frozen, nil
}
func (l *agentLease) save() error {
	return mdStateUpdate(l.path, func() ([]byte, error) { return json.Marshal(l.record) })
}
func (l *agentLease) finish(cause error) error {
	if l == nil || l.file == nil {
		return nil
	}
	now := time.Now().UTC()
	l.record.State = "stopped"
	l.record.Finished = &now
	l.record.Reason = "session ended"
	if cause != nil {
		l.record.Reason = "execution failed or cancelled"
	}
	err := l.save()
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(err, closeErr)
}

func ReadAgentInstances(directory string) ([]AgentInstance, error) {
	root, err := agentStateRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "instances")
	f, err := os.Open(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []AgentInstance{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(1025)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > 1024 {
		return nil, errors.New("agent inventory exceeds 1024 instances")
	}
	filter := ""
	if directory != "" {
		filter, err = filepath.Abs(directory)
		if err != nil {
			return nil, err
		}
	}
	rows := []AgentInstance{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "md-") {
			return nil, errors.New("invalid agent instance directory")
		}
		path := filepath.Join(dir, entry.Name())
		var r AgentInstance
		if err = mdStateRead(filepath.Join(path, "instance.json"), &r); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if r.SchemaVersion != 1 || r.ID != entry.Name() || r.ID != agentInstanceID(r.Source) || !agentVersionDigest.MatchString(r.Version) || r.Type != "md" || (r.State != "running" && r.State != "stopped") {
			return nil, errors.New("invalid agent instance record")
		}
		if filter != "" {
			rel, e := filepath.Rel(filter, r.Source)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
		}
		lock, e := openAgentLease(filepath.Join(path, "active.lock"), false)
		if e != nil {
			return nil, e
		}
		e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			if r.State == "running" {
				r.State = "stopped"
				r.Reason = "owner exited without recording completion"
				r.Finished = nil
			}
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		} else if !errors.Is(e, unix.EWOULDBLOCK) && !errors.Is(e, unix.EAGAIN) {
			lock.Close()
			return nil, e
		}
		lock.Close()
		r.Environment = "local"
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Started.After(rows[j].Started) })
	return rows, nil
}
func AgentInstanceStatus(out io.Writer, directory, output string) error {
	rows, err := ReadAgentInstances(directory)
	if err != nil {
		return err
	}
	if output == "json" {
		return json.NewEncoder(out).Encode(struct {
			Environment string          `json:"environment"`
			Agents      []AgentInstance `json:"agents"`
		}{"local", rows})
	}
	if output != "table" {
		return errors.New("status output must be table or json")
	}
	if len(rows) == 0 {
		_, err = fmt.Fprintln(out, "No agent instances. Start one with: kmx run FILE.md")
		return err
	}
	cells := [][]string{}
	for _, r := range rows {
		model := r.Model
		if model == "" {
			model = "default"
		}
		cells = append(cells, []string{r.Name, r.Type, r.Environment, r.State, r.Provider + " / " + model, mdShort(r.Version), fmt.Sprint(r.Generation), r.Source})
	}
	_, err = fmt.Fprintln(out, cliui.New(out).TableWithRoles([]string{"NAME", "TYPE", "ENVIRONMENT", "STATE", "INFERENCE", "VERSION", "RUN", "SOURCE"}, cells, []cliui.ColumnRole{cliui.ColumnText, cliui.ColumnText, cliui.ColumnText, cliui.ColumnState}))
	return err
}

func agentInstanceInventory(directory string) ([]agentTUIAgent, error) {
	instances, err := ReadAgentInstances(directory)
	if err != nil {
		return nil, err
	}
	rows := []agentTUIAgent{}
	for _, r := range instances {
		row := agentTUIAgent{Name: r.Name, Runtime: r.Type, Ready: r.State, Version: mdShort(r.Version), DefinitionPath: r.Source, TestsPath: MarkdownTestsPath(r.Source), Provider: r.Provider, Model: mdModelLabel(r.Model), PromptSource: "cached version " + r.Version, TestStatus: r.State + " · " + mdShort(r.Version)}
		root, e := agentStateRoot()
		if e != nil {
			return nil, e
		}
		b, e := mdRead(filepath.Join(root, "versions", strings.TrimPrefix(r.Version, "sha256:"), "agent.md"), markdownAgentLimit)
		if e == nil && mdHash(b) == r.Version {
			a, e := parseMarkdownAgent(r.Source, b)
			if e == nil {
				row.SystemPrompt = a.Instructions
			}
		} else {
			row.PromptError = "cached definition unavailable"
		}
		rows = append(rows, row)
	}
	return rows, nil
}
