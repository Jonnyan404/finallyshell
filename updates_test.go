package main

import (
	"encoding/json"
	"os"
	"testing"
)

// Installed copies accept only updates signed with the key whose public
// half is in mygo.json, read from the latest release of the repository
// there: changing either strands them at their version.
func TestUpdatesConfig(t *testing.T) {
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Updates struct {
			PublicKey  string `json:"publicKey"`
			GitHub     string `json:"github"`
			PrivateKey string `json:"privateKey"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	u := cfg.Updates
	if u.PublicKey != "fwcqse2POLiSdWytl0BHeURrztxcDObwmos+4IlTqHQ=" || u.GitHub != "TooonyChen/finallyshell" {
		t.Errorf("updates %+v", u)
	}
	if u.PrivateKey != "" {
		t.Errorf("the secret key is MYGO_UPDATER_PRIVATE_KEY in CI, not a file: %q", u.PrivateKey)
	}
}
