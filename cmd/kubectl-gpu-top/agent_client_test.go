package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeKubeconfig writes a minimal valid kubeconfig pointing at the given
// API server and returns its path.
func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	body := `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: ` + server + `
contexts:
- name: ctx
  context:
    cluster: c
    user: u
current-context: ctx
users:
- name: u
  user: {}
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestLoadRESTConfig_RespectsKUBECONFIG(t *testing.T) {
	const server = "https://from-env.example:6443"
	t.Setenv("KUBECONFIG", writeKubeconfig(t, server))

	cfg, err := loadRESTConfig("")
	if err != nil {
		t.Fatalf("loadRESTConfig: %v", err)
	}
	if cfg.Host != server {
		t.Errorf("host = %q, want %q (should load from $KUBECONFIG)", cfg.Host, server)
	}
}

func TestLoadRESTConfig_ExplicitFlagWinsOverKUBECONFIG(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t, "https://from-env.example:6443"))
	const explicit = "https://from-flag.example:6443"
	flagPath := writeKubeconfig(t, explicit)

	cfg, err := loadRESTConfig(flagPath)
	if err != nil {
		t.Fatalf("loadRESTConfig: %v", err)
	}
	if cfg.Host != explicit {
		t.Errorf("host = %q, want %q (explicit --kubeconfig should win)", cfg.Host, explicit)
	}
}
