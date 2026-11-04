package main

import (
	"encoding/json"
	"testing"
)

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]float64{"43k": 43000, "43000": 43000, "1,500": 1500, "12.5k": 12500} {
		got, err := parseAmount(in)
		if err != nil || got != want {
			t.Fatalf("%s: got %v %v want %v", in, got, err, want)
		}
	}
	if _, err := parseAmount("-5"); err == nil {
		t.Fatal("negative amount accepted")
	}
}

func TestMatchCategory(t *testing.T) {
	cs := []Category{{ID: "A", Name: "Food & Beverage"}, {ID: "B", Name: "Fees & Charges"}, {ID: "C", Name: "Transportation"}, {ID: "D", Name: "1.  Basic (primary)"}}
	for q, want := range map[string]string{"food & beverage": "A", "A": "A", "trans": "C", "beverage": "A", "1. Basic (primary)": "D"} {
		got, err := matchCategory(cs, q)
		if err != nil || got.ID != want {
			t.Fatalf("%s: got %v %v want %s", q, got.ID, err, want)
		}
	}
	if _, err := matchCategory(cs, "f"); err == nil {
		t.Fatal("ambiguous prefix accepted")
	}
}

func TestUnwrap(t *testing.T) {
	var out []Wallet
	if err := unwrap([]byte(`{"error":0,"data":[{"_id":"X","name":"Cash"}]}`), &out); err != nil || out[0].Name != "Cash" {
		t.Fatal(err, out)
	}
	if err := unwrap([]byte(`{"s":false,"e":706,"msg":"token rejected"}`), &out); err == nil {
		t.Fatal("abbreviated error envelope not caught")
	}
}

func TestIDList(t *testing.T) {
	var tx Transaction
	if err := json.Unmarshal([]byte(`{"campaign":[{"_id":"E1"}],"with":["mae"]}`), &tx); err != nil || tx.Campaign[0] != "E1" {
		t.Fatal(err, tx.Campaign)
	}
	if err := json.Unmarshal([]byte(`{"campaign":["E2"]}`), &tx); err != nil || tx.Campaign[0] != "E2" {
		t.Fatal(err, tx.Campaign)
	}
}
