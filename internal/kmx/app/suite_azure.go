package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Azure discovery is an explicit read-only action, never part of pane polling.
// Role assignments are candidates, not a proof of node network or pull access.
type suiteAzureCluster struct {
	Name, ID, ResourceGroup, FQDN, PrivateFQDN string
	IdentityProfile                            map[string]struct{ ObjectID string }
}
type suiteAzureRegistry struct {
	Name, ID, LoginServer string
	Pull                  string
}
type suiteAzureSelection struct {
	Cluster    suiteAzureCluster
	Context    string
	Registries []suiteAzureRegistry
}

func (a *App) discoverSuiteAKS(ctx context.Context) ([]suiteAzureSelection, error) {
	raw, err := a.liftDiscovery(ctx, "az", "aks", "list", "-o", "json", "--only-show-errors")
	if err != nil {
		return nil, err
	}
	var clusters []suiteAzureCluster
	if json.Unmarshal(raw, &clusters) != nil || len(clusters) > 128 {
		return nil, errors.New("invalid or oversized AKS discovery")
	}
	raw, err = a.liftDiscovery(ctx, "az", "acr", "list", "-o", "json", "--only-show-errors")
	if err != nil {
		return nil, err
	}
	var registries []suiteAzureRegistry
	if json.Unmarshal(raw, &registries) != nil || len(registries) > 256 {
		return nil, errors.New("invalid or oversized ACR discovery")
	}
	envs, err := a.agentTUIEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	var out []suiteAzureSelection
	for _, cluster := range clusters {
		for _, env := range envs {
			u, err := url.Parse(env.Server)
			if err != nil || env.Local || u.Hostname() == "" {
				continue
			}
			if !strings.EqualFold(u.Hostname(), cluster.FQDN) && !strings.EqualFold(u.Hostname(), cluster.PrivateFQDN) {
				continue
			}
			out = append(out, suiteAzureSelection{Cluster: cluster, Context: env.Name, Registries: registries})
		}
	}
	return out, nil
}

func (a *App) connectedSuiteACRs(ctx context.Context, s suiteAzureSelection) (suiteAzureSelection, error) {
	identity := s.Cluster.IdentityProfile["kubeletidentity"].ObjectID
	if identity == "" {
		for i := range s.Registries {
			s.Registries[i].Pull = "unknown (no kubelet identity)"
		}
		return s, nil
	}
	raw, err := a.liftDiscovery(ctx, "az", "role", "assignment", "list", "--assignee-object-id", identity, "--all", "--include-inherited", "-o", "json", "--only-show-errors")
	if err != nil {
		return s, err
	}
	var roles []struct{ Scope, RoleDefinitionName, Condition string }
	if json.Unmarshal(raw, &roles) != nil {
		return s, errors.New("invalid registry role observation")
	}
	for i := range s.Registries {
		r := &s.Registries[i]
		r.Pull = "unproven"
		for _, role := range roles {
			scope := strings.ToLower(strings.TrimRight(role.Scope, "/"))
			id := strings.ToLower(r.ID)
			if scope == "" || !(id == scope || strings.HasPrefix(id, scope+"/")) {
				continue
			}
			if role.RoleDefinitionName == "AcrPull" || role.RoleDefinitionName == "AcrPush" || role.RoleDefinitionName == "Container Registry Repository Reader" {
				r.Pull = "role observed; actual pull unproven"
				if role.Condition != "" {
					r.Pull = "conditional role; repository access unproven"
				}
				break
			}
		}
	}
	return s, nil
}

func suiteAzureDescription(rows []suiteAzureSelection) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "AKS: %s · context: %s\n", r.Cluster.Name, r.Context)
		for _, registry := range r.Registries {
			fmt.Fprintf(&b, "  ACR: %s · %s\n", registry.LoginServer, valueOr(registry.Pull, "connection not checked"))
		}
	}
	if len(rows) == 0 {
		return "No accessible AKS endpoint matches a configured context. Fetch credentials explicitly, then rediscover."
	}
	return b.String()
}

// Best-effort presentation enrichment only: exact endpoint matches preserve the
// kubeconfig identity. Azure metadata never supplies target access or ownership.
func (a *App) enrichLiftDestinations(ctx context.Context, choices []liftGuideChoice) []liftGuideChoice {
	out := append([]liftGuideChoice(nil), choices...)
	raw, err := a.liftDiscovery(ctx, "az", "account", "list", "--query", "[?state=='Enabled'].{name:name,id:id}", "-o", "json", "--only-show-errors")
	if err != nil {
		return out
	}
	var subs []struct{ Name, ID string }
	if json.Unmarshal(raw, &subs) != nil || len(subs) > 128 {
		return out
	}
	// Bounded to the active subscription; no multi-subscription fanout on entry.
	raw, err = a.liftDiscovery(ctx, "az", "aks", "list", "-o", "json", "--only-show-errors")
	if err != nil {
		return out
	}
	var clusters []suiteAzureCluster
	if json.Unmarshal(raw, &clusters) != nil || len(clusters) > 128 {
		return out
	}
	for i := range out {
		c := &out[i]
		if c.env.Local || c.env.Name == "" {
			continue
		}
		u, err := url.Parse(c.env.Server)
		if err != nil || u.Hostname() == "" {
			continue
		}
		var matches []suiteAzureCluster
		for _, cluster := range clusters {
			if strings.EqualFold(u.Hostname(), cluster.FQDN) || strings.EqualFold(u.Hostname(), cluster.PrivateFQDN) {
				matches = append(matches, cluster)
			}
		}
		if len(matches) != 1 {
			continue
		}
		cluster := matches[0]
		parts := strings.Split(strings.Trim(cluster.ID, "/"), "/")
		if len(parts) < 2 || !strings.EqualFold(parts[0], "subscriptions") {
			continue
		}
		sub := parts[1]
		for _, s := range subs {
			if strings.EqualFold(s.ID, sub) {
				sub = s.Name + " · " + s.ID
				break
			}
		}
		c.detail = "AKS: " + cluster.Name + "\nResource group: " + cluster.ResourceGroup + "\nSubscription: " + sub + "\nEndpoint: " + c.env.Server
	}
	return out
}
