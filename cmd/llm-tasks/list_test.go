package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const sampleConfig = `
common:
  api:
    endpoint: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  defaults:
    attempts: 1
    timeout_seconds: 1

models:
  - name: gpt-5-mini
    provider: openai
    model_id: gpt-5-mini
    default: true
    supports_temperature: false
    default_temperature: 1
    max_completion_tokens: 1500

recipes:
  - name: changelog
    enabled: true
    model: gpt-5-mini
    type: task/changelog
    inputs: { }
    recipe: { }
    apply: { }
  - name: sort
    enabled: false
    model: gpt-5-mini
    type: task/sort
    grant: { }
    projects: [ ]
    thresholds: { }
`

func writeTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestRootList_DefaultFiltersDisabled(t *testing.T) {
	cfg := writeTempConfig(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"list", "--config", cfg})
	t.Cleanup(func() { rootCmd.SetArgs([]string{}) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute list: %v\nstdout:\n%s", err, out.String())
	}

	got := out.String()
	if !bytes.Contains([]byte(got), []byte("changelog")) {
		t.Fatalf("expected to list enabled recipe 'changelog'; got:\n%s", got)
	}
	if bytes.Contains([]byte(got), []byte("sort")) {
		t.Fatalf("did not expect disabled recipe 'sort' without --all; got:\n%s", got)
	}
}

func TestRootList_AllShowsDisabled(t *testing.T) {
	cfg := writeTempConfig(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"list", "--config", cfg, "--all"})
	t.Cleanup(func() { rootCmd.SetArgs([]string{}) })

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute list --all: %v\nstdout:\n%s", err, out.String())
	}

	got := out.String()
	if !bytes.Contains([]byte(got), []byte("changelog")) || !bytes.Contains([]byte(got), []byte("sort")) {
		t.Fatalf("expected to list both 'changelog' and 'sort'; got:\n%s", got)
	}
}
