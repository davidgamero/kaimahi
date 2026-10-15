package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/guard"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

type suiteContextChoice struct {
	label, detail, action string
	env                   agentTUIEnvironment
	cluster               suiteAzureCluster
}
type suiteContextPicker struct {
	side      int
	aks       bool
	query     string
	selection int
	choices   []suiteContextChoice
}

func (p *suiteContextPicker) matches() []int {
	items := make([]chatPickerItem, len(p.choices))
	for i, c := range p.choices {
		items[i] = chatPickerItem{name: c.label, detail: c.detail}
	}
	return (chatPicker{items: items, query: p.query, searchEnabled: true}).matches()
}

func (m *suiteConsole) openContexts(side int) tea.Cmd {
	return m.jobNamed("Loading available environment contexts", func(ctx context.Context, a *App) suiteConsoleResult {
		envs, err := a.agentTUIEnvironments(ctx)
		p := &suiteContextPicker{side: side}
		for _, env := range envs {
			if env.Local == (side == 0) {
				p.choices = append(p.choices, suiteContextChoice{label: env.Name, detail: env.Server, env: env})
			}
		}
		if side == 0 {
			if _, e := exec.LookPath("kind"); e == nil {
				raw, e := a.azureCapture(ctx, 15*time.Second, "kind", []string{"get", "clusters"}, "kind inventory unavailable; check the container engine")
				err = errors.Join(err, e)
				if e == nil {
					for _, name := range strings.Fields(string(raw)) {
						if scaffold.ValidateName(name) != nil {
							continue
						}
						found := false
						for _, c := range p.choices {
							if c.env.Name == "kind-"+name {
								found = true
							}
						}
						if !found {
							p.choices = append(p.choices, suiteContextChoice{label: "kind-" + name, detail: "Existing kind cluster · configure access", action: "kind", env: agentTUIEnvironment{Name: "kind-" + name, Local: true}})
						}
					}
				}
			}
			p.choices = append(p.choices, suiteContextChoice{label: "Initialize local environment", detail: "kind + Ollama + Orka via local up; explicit confirmation", action: "init"})
		} else {
			p.choices = append(p.choices, suiteContextChoice{label: "Discover AKS clusters", detail: "Use Azure login to select an existing AKS cluster", action: "aks"})
		}
		return suiteConsoleResult{contexts: p, err: err}
	})
}

func (m *suiteConsole) discoverContextAKS() tea.Cmd {
	return m.jobNamed("Discovering AKS clusters in the active Azure subscription", func(ctx context.Context, a *App) suiteConsoleResult {
		raw, err := a.liftDiscovery(ctx, "az", "aks", "list", "-o", "json", "--only-show-errors")
		if err != nil {
			return suiteConsoleResult{err: err}
		}
		var clusters []suiteAzureCluster
		if json.Unmarshal(raw, &clusters) != nil || len(clusters) > 128 {
			return suiteConsoleResult{err: errors.New("invalid AKS inventory")}
		}
		p := &suiteContextPicker{side: 1, aks: true}
		for _, c := range clusters {
			p.choices = append(p.choices, suiteContextChoice{label: c.Name, detail: c.ResourceGroup + " · " + c.ID, action: "connect-aks", cluster: c})
		}
		return suiteConsoleResult{contexts: p, text: "Select AKS from the active Azure subscription; credentials are fetched only after confirmation"}
	})
}

func (m *suiteConsole) selectContext(env agentTUIEnvironment, side int) tea.Cmd {
	m.contexts = nil
	if side == 0 {
		m.opt.LocalContext = env.Name
	} else {
		m.opt.RemoteContext = env.Name
	}
	m.columns[side].Env = env
	m.selectSide(side)
	key := "tui/local"
	if side == 1 {
		key = "tui/remote"
	}
	if err := saveLiftPreference(key, env.Name); err != nil {
		m.status = "Context selected; preference not saved: " + err.Error()
	}
	return m.refreshLive()
}

func (m *suiteConsole) updateContexts(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.contexts
	switch key.String() {
	case "esc":
		m.contexts = nil
		return m, nil
	case "alt+left":
		if p.aks {
			return m, m.openContexts(1)
		}
		m.contexts = nil
		return m, nil
	case "up":
		p.selection = max(0, p.selection-1)
	case "down":
		p.selection = min(max(0, len(p.matches())-1), p.selection+1)
	case "backspace":
		r := []rune(p.query)
		if len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.selection = 0
		} else if p.aks {
			return m, m.openContexts(1)
		} else {
			m.contexts = nil
		}
	case "n":
		if p.side == 1 && !p.aks {
			return m, m.discoverContextAKS()
		}
		p.query += "n"
		p.selection = 0
	case "enter":
		matches := p.matches()
		if len(matches) == 0 {
			return m, nil
		}
		c := p.choices[matches[min(p.selection, len(matches)-1)]]
		switch c.action {
		case "aks":
			return m, m.discoverContextAKS()
		case "init":
			m.contexts = nil
			return m, m.startForm("local-init", []string{"Local kind cluster name", "Type initialize to prepare kind, Ollama and Orka"}, []string{valueOr(m.a.Cfg.KindCluster, "kaimahi"), ""})
		case "kind":
			m.contexts = nil
			return m, m.startForm("kind-connect", []string{"Existing kind cluster", "Type connect to save local access"}, []string{strings.TrimPrefix(c.env.Name, "kind-"), ""})
		case "connect-aks":
			m.pendingAKS = c.cluster
			m.contexts = nil
			return m, m.startForm("aks-connect", []string{"Type cluster name to fetch private kubeconfig: " + c.cluster.Name}, []string{""})
		default:
			return m, m.selectContext(c.env, p.side)
		}
	default:
		if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && len(p.query)+len(key.Text) <= 512 {
			p.query += key.Text
			p.selection = 0
		}
	}
	return m, nil
}

// Imported access is private and independent of agent/deployment records. No
// default kubeconfig overwrite or current-context mutation is needed.
func suiteContextDirectory() (string, error) {
	base, err := config.StateDir()
	return filepath.Join(base, "console-contexts"), err
}
func (a *App) prepareConsoleContext(ctx context.Context, kindName string, cluster *suiteAzureCluster, initialize bool) (agentTUIEnvironment, error) {
	var env agentTUIEnvironment
	if cluster == nil && scaffold.ValidateName(kindName) != nil {
		return env, errors.New("invalid kind cluster name")
	}
	if cluster == nil {
		if err := a.preflight(depKind); err != nil {
			return env, err
		}
	}
	dir, err := suiteContextDirectory()
	if err != nil {
		return env, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return env, err
	}
	f, err := os.CreateTemp(dir, "access-*.kubeconfig")
	if err != nil {
		return env, err
	}
	path := f.Name()
	if err = f.Close(); err != nil {
		return env, err
	}
	env = agentTUIEnvironment{Name: "kind-" + kindName, Kubeconfig: path, Local: cluster == nil}
	if cluster != nil {
		parts := strings.Split(strings.Trim(cluster.ID, "/"), "/")
		if len(parts) != 8 || !strings.EqualFold(parts[0], "subscriptions") || !strings.EqualFold(parts[2], "resourceGroups") || !strings.EqualFold(parts[3], cluster.ResourceGroup) || !strings.EqualFold(parts[4], "providers") || !strings.EqualFold(parts[5], "Microsoft.ContainerService") || !strings.EqualFold(parts[6], "managedClusters") || !strings.EqualFold(parts[7], cluster.Name) {
			return env, errors.New("invalid AKS resource identity")
		}
		env.Name = cluster.Name
		_, err = a.azureCapture(ctx, 90*time.Second, "az", []string{"aks", "get-credentials", "--subscription", parts[1], "--resource-group", cluster.ResourceGroup, "--name", cluster.Name, "--file", path, "--context", cluster.Name, "--overwrite-existing", "--only-show-errors"}, "AKS credential retrieval failed; check login and access")
	} else if initialize {
		w := env.app(a).withRunContext(ctx)
		w.Cfg.KindCluster = kindName
		w.Cfg.Confirm = env.Name
		err = w.Up("")
	} else {
		_, err = a.azureCapture(ctx, 30*time.Second, "kind", []string{"export", "kubeconfig", "--name", kindName, "--kubeconfig", path}, "kind access export failed")
	}
	if err != nil {
		return env, err
	}
	raw, err := env.app(a).orkaCapture(ctx, nil, "config", "view", "-o", "json")
	if err != nil {
		return env, err
	}
	kube, err := guard.ParseKubeconfig(raw)
	if err != nil {
		return env, err
	}
	p, err := guard.Classify(kube, env.Name)
	if err != nil || p.Host == "" || p.Local != env.Local {
		return env, errors.New("retrieved context does not match selected environment")
	}
	env.Server = consoleContextServer(kube, env.Name)
	if cluster != nil && !strings.EqualFold(p.Host, cluster.FQDN) && !strings.EqualFold(p.Host, cluster.PrivateFQDN) {
		return env, errors.New("AKS endpoint differs from selected cluster")
	}
	metadata, _ := json.Marshal(env)
	if err = writePrivateAgentFile(path+".json", metadata); err != nil {
		return env, err
	}
	return env, nil
}

func (a *App) savedConsoleContexts(ctx context.Context) ([]agentTUIEnvironment, error) {
	dir, err := suiteContextDirectory()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(files) > 512 {
		return nil, fmt.Errorf("console context inventory exceeds limit")
	}
	var result []agentTUIEnvironment
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") || file.IsDir() {
			continue
		}
		path := filepath.Join(dir, file.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return result, errors.New("saved console context must be a private regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return result, err
		}
		var env agentTUIEnvironment
		if json.Unmarshal(raw, &env) != nil {
			return result, errors.New("invalid saved console context")
		}
		if env.Name == "" || env.Kubeconfig != strings.TrimSuffix(path, ".json") {
			return result, errors.New("invalid saved console access path")
		}
		info, err = os.Lstat(env.Kubeconfig)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return result, errors.New("console kubeconfig must be a private regular file")
		}
		raw, err = env.app(a).orkaCapture(ctx, nil, "config", "view", "-o", "json")
		if err != nil {
			continue
		}
		kube, err := guard.ParseKubeconfig(raw)
		if err != nil {
			continue
		}
		p, err := guard.Classify(kube, env.Name)
		if err != nil || p.Host == "" {
			continue
		}
		env.Local = p.Local
		env.Server = consoleContextServer(kube, env.Name)
		result = append(result, env)
	}
	return result, nil
}

func (m *suiteConsole) contextApp() *App {
	paths := []string{}
	for _, col := range m.columns {
		if col.Env.Kubeconfig != "" {
			paths = append(paths, filepath.SplitList(col.Env.Kubeconfig)...)
		}
	}
	if len(paths) == 0 {
		return m.a
	}
	base := os.Getenv("KUBECONFIG")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".kube", "config")
	}
	paths = append(paths, filepath.SplitList(base)...)
	return (agentTUIEnvironment{Name: m.a.Cfg.KubeContext, Kubeconfig: strings.Join(paths, string(os.PathListSeparator))}).app(m.a)
}
