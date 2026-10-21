package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const localAgentManagerVersion = "copilot-sessions/v1"

type agentManagerRecord struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
	Socket  string `json:"socket"`
}
type managedAgent struct {
	lease      *agentLease
	definition *MarkdownAgent
	executor   markdownExecutor
	ctx        context.Context
	cancel     context.CancelFunc
	turn       sync.Mutex
	stopped    bool // protected by turn
}
type localAgentManager struct {
	mu          sync.Mutex
	agents      map[string]*managedAgent
	newExecutor func() markdownExecutor
	socket      string
	lastActive  time.Time
	closing     bool
}

func (s *MarkdownSession) StartBackgroundAgent(ctx context.Context, a *MarkdownAgent) (AgentInstance, error) {
	if len(a.Tools) > 0 {
		return AgentInstance{}, errors.New("ordinary run requires tool-free agents; mocked tools are test-only")
	}
	if err := s.checkSelection(a); err != nil {
		return AgentInstance{}, err
	}
	if existing, err := FindAgentInstance(a.Path); err == nil && existing.State == "running" {
		if existing.Manager != localAgentManagerVersion {
			return AgentInstance{}, fmt.Errorf("%s uses a dedicated worker; stop it and run again to use the shared manager", existing.Name)
		}
		if existing.Version != a.Revision || existing.Model != s.effective(a).Model {
			return AgentInstance{}, fmt.Errorf("agent %s is running a different version; stop it before updating", existing.Name)
		}
		if _, err = agentWorkerCall(ctx, existing, "ping", ""); err != nil {
			return AgentInstance{}, err
		}
		return existing, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return AgentInstance{}, err
	}
	if _, err := CacheMarkdownVersion(a); err != nil {
		return AgentInstance{}, err
	}
	manager, err := ensureAgentManager(ctx)
	if err != nil {
		return AgentInstance{}, err
	}
	// Source identity is canonical and version is checked again by the manager.
	source, err := filepath.EvalSymlinks(a.Path)
	if err != nil {
		return AgentInstance{}, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return AgentInstance{}, err
	}
	reply, err := agentManagerCall(ctx, manager.Socket, agentWorkerRequest{Operation: "start", Source: source, Version: a.Revision, Model: s.effective(a).Model})
	if err != nil {
		return AgentInstance{}, err
	}
	if reply.Instance == nil {
		return AgentInstance{}, errors.New("manager returned no instance")
	}
	return *reply.Instance, nil
}

func ensureAgentManager(ctx context.Context) (agentManagerRecord, error) {
	root, err := agentStateRoot()
	if err != nil {
		return agentManagerRecord{}, err
	}
	dir, err := filepath.Abs(filepath.Join(root, "manager"))
	if err != nil {
		return agentManagerRecord{}, err
	}
	if err = mdPrivateDir(dir); err != nil {
		return agentManagerRecord{}, err
	}
	lease, err := openAgentLease(filepath.Join(dir, "active.lock"), true)
	if err != nil {
		return agentManagerRecord{}, err
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		err = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			lease.Close()
			return agentManagerRecord{}, err
		}
		var record agentManagerRecord
		if e := mdStateRead(filepath.Join(dir, "manager.json"), &record); e == nil && record.Version == localAgentManagerVersion {
			probe, cancel := context.WithTimeout(ctx, time.Second)
			_, e = agentManagerCall(probe, record.Socket, agentWorkerRequest{Operation: "manager-ping"})
			cancel()
			if e == nil {
				lease.Close()
				return record, nil
			}
		}
		select {
		case <-ctx.Done():
			lease.Close()
			return agentManagerRecord{}, ctx.Err()
		case <-deadline.C:
			lease.Close()
			return agentManagerRecord{}, errors.New("local agent manager is unavailable; existing agent state was preserved")
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer lease.Close()
	exe, err := os.Executable()
	if err != nil {
		return agentManagerRecord{}, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return agentManagerRecord{}, err
	}
	defer read.Close()
	defer write.Close()
	cmd := exec.Command(exe, "__agent-manager")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.ExtraFiles = []*os.File{lease, write}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	// State paths passed as data, never shell interpolation; no credentials copied.
	input, err := cmd.StdinPipe()
	if err != nil {
		return agentManagerRecord{}, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		return agentManagerRecord{}, err
	}
	write.Close()
	if err = json.NewEncoder(input).Encode(dir); err != nil {
		input.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return agentManagerRecord{}, err
	}
	input.Close()
	replies := make(chan agentWorkerReply, 1)
	go func() {
		var r agentWorkerReply
		if json.NewDecoder(io.LimitReader(read, 8192)).Decode(&r) != nil {
			r.Error = "manager exited before readiness"
		}
		replies <- r
	}()
	var reply agentWorkerReply
	select {
	case reply = <-replies:
	case <-ctx.Done():
		reply.Error = "manager startup cancelled"
	case <-deadline.C:
		reply.Error = "manager startup timed out"
	}
	if !reply.Ready {
		cmd.Process.Kill()
		cmd.Wait()
		return agentManagerRecord{}, errors.New(reply.Error)
	}
	go func() { _ = cmd.Wait() }()
	var record agentManagerRecord
	if err = mdStateRead(filepath.Join(dir, "manager.json"), &record); err != nil {
		return record, err
	}
	return record, nil
}

func agentManagerCall(ctx context.Context, socket string, request agentWorkerRequest) (agentWorkerReply, error) {
	// Reuse the same socket validation and deadlines as per-agent attachment.
	return agentSocketCall(ctx, socket, request)
}

func RunAgentManager() error {
	lease := os.NewFile(3, "manager-lease")
	ready := os.NewFile(4, "manager-ready")
	defer lease.Close()
	defer ready.Close()
	notify := func(err error) {
		reply := agentWorkerReply{Ready: err == nil}
		if err != nil {
			reply.Error = err.Error()
		}
		_ = json.NewEncoder(ready).Encode(reply)
	}
	var dir string
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 8192)).Decode(&dir); err != nil {
		notify(err)
		return err
	}
	root, err := agentStateRoot()
	if err != nil {
		notify(err)
		return err
	}
	expected, err := filepath.Abs(filepath.Join(root, "manager"))
	if err != nil || dir != expected {
		err = errors.New("manager state identity mismatch")
		notify(err)
		return err
	}
	info, err := lease.Stat()
	if err != nil {
		notify(err)
		return err
	}
	disk, err := os.Lstat(filepath.Join(dir, "active.lock"))
	if err != nil || !os.SameFile(info, disk) {
		err = errors.New("manager lease mismatch")
		notify(err)
		return err
	}
	if err = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		notify(err)
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	provider := &markdownCopilot{}
	defer provider.Close()
	if _, err = provider.Start(ctx); err != nil {
		notify(err)
		return err
	}
	socketDir, err := os.MkdirTemp("", "kmx-manager-")
	if err != nil {
		notify(err)
		return err
	}
	defer os.RemoveAll(socketDir)
	socket := filepath.Join(socketDir, "rpc.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		notify(err)
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socket, 0600); err != nil {
		notify(err)
		return err
	}
	manager := &localAgentManager{agents: map[string]*managedAgent{}, socket: socket, lastActive: time.Now(), newExecutor: func() markdownExecutor {
		return &markdownCopilot{client: provider.client, dir: provider.dir, version: provider.version, borrowed: true}
	}}
	defer manager.close()
	record := agentManagerRecord{Version: localAgentManagerVersion, PID: os.Getpid(), Socket: socket}
	if err = mdStateUpdate(filepath.Join(dir, "manager.json"), func() ([]byte, error) { return json.Marshal(record) }); err != nil {
		notify(err)
		return err
	}
	notify(nil)
	ready.Close()
	go func() { <-ctx.Done(); listener.Close() }()
	// Release the shared provider when the final agent has stopped for 30 seconds.
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				manager.mu.Lock()
				idle := len(manager.agents) == 0 && time.Since(manager.lastActive) > 30*time.Second
				if idle {
					manager.closing = true
				}
				manager.mu.Unlock()
				if idle {
					cancel()
					return
				}
			}
		}
	}()
	var jobs sync.WaitGroup
	defer func() { cancel(); jobs.Wait() }()
	for {
		conn, e := listener.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var req agentWorkerRequest
			if json.NewDecoder(io.LimitReader(conn, 128<<10)).Decode(&req) != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(6 * time.Minute))
			reply := manager.handle(ctx, req)
			_ = json.NewEncoder(conn).Encode(reply)
		}()
	}
}

func (m *localAgentManager) handle(ctx context.Context, req agentWorkerRequest) agentWorkerReply {
	fail := func(err error) agentWorkerReply { return agentWorkerReply{Error: err.Error()} }
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return fail(errors.New("manager is stopping; run again"))
	}
	m.lastActive = time.Now()
	if req.Operation == "manager-ping" {
		m.mu.Unlock()
		return agentWorkerReply{Ready: true}
	}
	if req.Operation == "start" {
		defer m.mu.Unlock()
		if !agentVersionDigest.MatchString(req.Version) || !filepath.IsAbs(req.Source) || (req.Model != "" && !localModelName.MatchString(req.Model)) {
			return fail(errors.New("invalid agent start request"))
		}
		id := agentInstanceID(req.Source)
		if active := m.agents[id]; active != nil {
			r := active.lease.record
			if r.Version != req.Version || r.Model != req.Model {
				return fail(errors.New("agent already running another version; stop before updating"))
			}
			return agentWorkerReply{Ready: true, Instance: &r}
		}
		a, err := LoadMarkdownAgent(req.Source, "")
		if err != nil {
			return fail(err)
		}
		if a.Revision != req.Version {
			return fail(errors.New("source changed before manager start; run again"))
		}
		if len(a.Tools) > 0 {
			return fail(errors.New("background agents must be tool-free"))
		}
		lease, frozen, err := beginAgentInstance(a, req.Model)
		if err != nil {
			return fail(err)
		}
		executor := m.newExecutor()
		agentCtx, cancel := context.WithCancel(ctx)
		lease.record.Socket = m.socket
		lease.record.Manager = localAgentManagerVersion
		if err = lease.save(); err != nil {
			cancel()
			executor.Close()
			lease.finish(err)
			return fail(err)
		}
		m.agents[id] = &managedAgent{lease: lease, definition: frozen, executor: executor, ctx: agentCtx, cancel: cancel}
		r := lease.record
		return agentWorkerReply{Ready: true, Instance: &r}
	}
	agent := m.agents[req.AgentID]
	if agent == nil {
		m.mu.Unlock()
		return fail(errors.New("agent session is stopped or unknown"))
	}
	r := agent.lease.record
	if req.Version != r.Version || req.Generation != r.Generation {
		m.mu.Unlock()
		return fail(errors.New("agent version changed; reconnect"))
	}
	if req.Operation == "stop" {
		// Keep the manager mutex while stop drains this agent; no replacement can
		// acquire its source lease until cleanup and record finalization complete.
		agent.cancel()
		agent.turn.Lock()
		agent.stopped = true
		agent.executor.Close()
		err := agent.lease.finish(nil)
		agent.turn.Unlock()
		delete(m.agents, req.AgentID)
		m.lastActive = time.Now()
		m.mu.Unlock()
		if err != nil {
			return fail(err)
		}
		return agentWorkerReply{Ready: true}
	}
	if req.Operation == "ping" {
		m.mu.Unlock()
		return agentWorkerReply{Ready: true}
	}
	if req.Operation != "send" {
		m.mu.Unlock()
		return fail(errors.New("unsupported manager operation"))
	}
	if len(req.Prompt) > 64<<10 || req.Prompt == "" {
		m.mu.Unlock()
		return fail(errors.New("prompt must contain 1-65536 bytes"))
	}
	if !agent.turn.TryLock() {
		m.mu.Unlock()
		return fail(errors.New("agent is busy with another turn"))
	}
	m.mu.Unlock()
	defer agent.turn.Unlock()
	if agent.stopped {
		return fail(errors.New("agent stopped"))
	}
	answer, err := agent.executor.Answer(agent.ctx, agent.definition, req.Prompt, nil)
	if err != nil {
		return fail(err)
	}
	return agentWorkerReply{Ready: true, Answer: answer.Text}
}
func (m *localAgentManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closing = true
	for id, a := range m.agents {
		a.cancel()
		a.turn.Lock()
		a.executor.Close()
		_ = a.lease.finish(nil)
		a.stopped = true
		a.turn.Unlock()
		delete(m.agents, id)
	}
}
