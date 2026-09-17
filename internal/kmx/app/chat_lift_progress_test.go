package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAzureFetchLabelsIdentifyPhaseAndScope(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"account", "show"}, "Fetching active Azure tenant"},
		{[]string{"group", "list", "--subscription", "sub"}, "Fetching resource groups · subscription:sub"},
		{[]string{"aks", "list", "--subscription", "sub", "--resource-group", "rg"}, "Fetching AKS clusters · subscription:sub · rg:rg"},
		{[]string{"aks", "get-credentials", "--name", "cluster"}, "Fetching AKS credentials · resource:cluster"},
		{[]string{"cognitiveservices", "account", "list-models", "--name", "foundry"}, "Fetching available Foundry models · resource:foundry"},
		{[]string{"cognitiveservices", "account", "deployment", "list"}, "Fetching Foundry deployments"},
		{[]string{"cognitiveservices", "account", "keys", "list"}, "Fetching Foundry credential"},
		{[]string{"cognitiveservices", "usage", "list", "--subscription", "sub", "--location", "eastus"}, "Checking available Foundry quota · subscription:sub · region:eastus"},
		{[]string{"cognitiveservices", "model", "list"}, "Fetching regional Foundry models"},
		{[]string{"rest", "--method", "get"}, "Fetching Azure REST response"},
	} {
		if got := azureFetchLabel(tc.args); got != tc.want {
			t.Fatalf("got=%q want=%q", got, tc.want)
		}
	}
}

func TestLiftSubscriptionsReportsTenantWithoutFilteringOtherTenants(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "az", `case "$*" in
  *"account show"*) printf '"tenant-one"' ;;
  *"account list"*) printf '[{"name":"Subscription","id":"sub"}]' ;;
  *) exit 1 ;;
esac`)
	t.Setenv("PATH", dir)
	var out bytes.Buffer
	b := &orkaChatBackend{app: &App{Out: &out}}
	raw, err := b.liftSubscriptions(t.Context())
	if err != nil || !strings.Contains(string(raw), "Subscription") {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
	if !strings.Contains(out.String(), "Fetching subscriptions for tenant:tenant-one") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestLiftFetchPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := liftFetchProgress(ctx, &bytes.Buffer{}, "Fetching", func(ctx context.Context) ([]byte, error) { return nil, ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
