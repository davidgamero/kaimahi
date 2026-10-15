package app

import (
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

func TestSuiteACRConnectionRequiresMatchingScopeAndPreservesUnknown(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "az", `printf '%s' '[{"scope":"/subscriptions/test/resourceGroups/group/providers/Microsoft.ContainerRegistry/registries/connected","roleDefinitionName":"AcrPull"},{"scope":"/subscriptions/test/resourceGroups/group/providers/Microsoft.ContainerRegistry/registries/conditional","roleDefinitionName":"Container Registry Repository Reader","condition":"repository constraint"}]'`)
	t.Setenv("PATH", dir)
	a := &App{Run: &run.Runner{}}
	s := suiteAzureSelection{Cluster: suiteAzureCluster{IdentityProfile: map[string]struct{ ObjectID string }{"kubeletidentity": {ObjectID: "test-principal"}}}}
	for _, name := range []string{"connected", "connected-lookalike", "conditional"} {
		s.Registries = append(s.Registries, suiteAzureRegistry{Name: name, ID: "/subscriptions/test/resourceGroups/group/providers/Microsoft.ContainerRegistry/registries/" + name})
	}
	got, err := a.connectedSuiteACRs(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Registries[0].Pull, "role observed") || got.Registries[1].Pull != "unproven" || !strings.HasPrefix(got.Registries[2].Pull, "conditional") {
		t.Fatalf("connections=%+v", got.Registries)
	}
	s.Cluster.IdentityProfile = nil
	got, err = a.connectedSuiteACRs(t.Context(), s)
	if err != nil || !strings.HasPrefix(got.Registries[0].Pull, "unknown") {
		t.Fatalf("missing identity: %+v %v", got, err)
	}
}

func TestLiftAzureEnrichmentPreservesExactContext(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "az", `case "$1 $2" in
"account list") printf '%s' '[{"name":"Development","id":"sub-id"}]';;
"aks list") printf '%s' '[{"name":"cluster","resourceGroup":"rg-west","id":"/subscriptions/sub-id/resourceGroups/rg-west/providers/Microsoft.ContainerService/managedClusters/cluster","fqdn":"exact.example"}]';;
*) exit 1;; esac`)
	t.Setenv("PATH", dir)
	a := &App{Run: &run.Runner{}}
	choices := []liftGuideChoice{{label: "REMOTE · chosen", env: agentTUIEnvironment{Name: "chosen", Server: "https://exact.example"}}, {label: "REMOTE · similar", env: agentTUIEnvironment{Name: "similar", Server: "https://not-exact.example"}, detail: "original"}}
	got := a.enrichLiftDestinations(t.Context(), choices)
	if got[0].env.Name != "chosen" || !strings.Contains(got[0].detail, "rg-west") || !strings.Contains(got[0].detail, "Development · sub-id") || got[1].detail != "original" {
		t.Fatalf("wrong enrichment %+v", got)
	}
}
