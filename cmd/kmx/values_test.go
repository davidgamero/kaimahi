package main

import "testing"

func TestBudgetValuesPreserveNilAndZero(t *testing.T) {
	cents, tokens, err := parseBudgetValues("-", "-")
	if err != nil || cents != nil || tokens != nil {
		t.Fatalf("clear values: %v %v %v", cents, tokens, err)
	}
	cents, tokens, err = parseBudgetValues("0", "1")
	if err != nil || cents == nil || *cents != 0 || tokens == nil || *tokens != 1 {
		t.Fatalf("bounded values: %v %v %v", cents, tokens, err)
	}
}

func TestApprovalValuesPreserveAbsentBounds(t *testing.T) {
	ttl, uses, amount, err := parseApprovalValues("-", "1", "-")
	if err != nil || ttl != nil || uses == nil || *uses != 1 || amount != nil {
		t.Fatalf("values: %v %v %v %v", ttl, uses, amount, err)
	}
}

func TestRequestCredentialDefaultsByKind(t *testing.T) {
	if got := requestCredential("", "tool", "model", "tools"); got != "tools" {
		t.Fatal(got)
	}
	if got := requestCredential(" ", "budget", "model", "tools"); got != "model" {
		t.Fatal(got)
	}
	if got := requestCredential("explicit", "tool", "model", "tools"); got != "explicit" {
		t.Fatal(got)
	}
}
