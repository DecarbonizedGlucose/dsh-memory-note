package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDshProfileRegisterAndRemove drives the real dsh CLI: install the
// adapter bundle into a fresh profile under a /tmp dsh home, verify the
// composed config tree and a real boot, then uninstall and verify removal.
// Skipped when the dsh CLI is not installed.
func TestDshProfileRegisterAndRemove(t *testing.T) {
	dsh, err := exec.LookPath("dsh")
	if err != nil {
		t.Skip("dsh CLI not installed; skipping profile install test")
	}
	pnpm, err := exec.LookPath("pnpm")
	if err != nil {
		t.Skip("pnpm not installed; skipping profile install test")
	}

	repoRoot, err := filepath.Abs(filepath.Join(".."))
	if err != nil {
		t.Fatal(err)
	}

	// The documented prepare step: the bundle must be built before a local
	// checkout install.
	build := exec.Command(pnpm, "--dir", filepath.Join(repoRoot, "adapter"), "build")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("adapter build failed: %v\n%s", err, output)
	}

	// A dedicated dsh home under the OS temporary directory, removed after
	// the test.
	home, err := os.MkdirTemp("", "dsh-memory-note-test-dsh-home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	profileDir := filepath.Join(home, "profiles", "memtest")
	storeDir := filepath.Join(home, "pnpm-store")

	runDsh := func(name string, args ...string) string {
		t.Helper()
		command := exec.Command(dsh, args...)
		command.Dir = repoRoot
		command.Env = append(os.Environ(), "DSH_HOME="+home)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %v\n%s", name, err, output)
		}
		return string(output)
	}

	// Register the local checkout. dsh plugin forwards to pnpm; the store is
	// pinned into the temp home so the test never touches the user's real
	// pnpm store.
	runDsh("register", "plugin", "--profile", "memtest", "add", "./adapter", "--store-dir", storeDir)

	manifest := readManifest(t, profileDir)
	assertBundles(t, manifest, "@deepseek-ai/dsh-base", "dsh-memory-note")
	if dependency, ok := manifest.Dependencies["dsh-memory-note"]; !ok || !strings.HasPrefix(dependency, "link:") {
		t.Fatalf("manifest dependency = %v, want link: to the adapter checkout", manifest.Dependencies)
	}

	config := runDsh("dump-config", "--profile", "memtest", "--dump-config")
	if !strings.Contains(config, "# == dsh-memory-note") || !strings.Contains(config, "name: dsh-memory-note") {
		t.Fatalf("composed config missing memory-note layer:\n%s", config)
	}

	// Real boot: the plugin must load and register its tools. The Go core is
	// found through PATH via the default binaryPath config. The harness is a
	// resident process (HMR watcher keeps it alive), so the test waits for
	// the plugin load line and then kills it.
	bootContext, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	boot := exec.CommandContext(bootContext, dsh, "--profile", "memtest")
	boot.Dir = repoRoot
	boot.Env = append(os.Environ(),
		"DSH_HOME="+home,
		"PATH="+filepath.Dir(coreBinary)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	stdout, err := boot.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	boot.Stderr = &stderr
	// Killing the direct process does not kill its descendants, which can keep
	// the stdout/stderr pipes open and make Wait block forever. WaitDelay makes
	// Wait return once the direct process is reaped even if a descendant still
	// holds a pipe.
	boot.WaitDelay = 5 * time.Second
	if err := boot.Start(); err != nil {
		t.Fatalf("start profile boot: %v", err)
	}
	loaded := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "[memory-note] plugin loaded: 13 tools registered") {
				loaded <- nil
				return
			}
		}
		loaded <- fmt.Errorf("boot stdout closed before plugin load: %v", scanner.Err())
	}()
	select {
	case err := <-loaded:
		if err != nil {
			t.Fatalf("%v; stderr=%s", err, stderr.String())
		}
	case <-bootContext.Done():
		t.Fatalf("profile boot did not report plugin load within 60s; stderr=%s", stderr.String())
	}
	cancel()
	_ = boot.Process.Kill()
	_ = boot.Wait()

	// Unregister.
	runDsh("remove", "plugin", "--profile", "memtest", "remove", "dsh-memory-note", "--store-dir", storeDir)
	manifest = readManifest(t, profileDir)
	assertBundles(t, manifest, "@deepseek-ai/dsh-base")
	if _, ok := manifest.Dependencies["dsh-memory-note"]; ok {
		t.Fatalf("dependency still present after remove: %v", manifest.Dependencies)
	}
}

type profileManifest struct {
	Dependencies map[string]string `json:"dependencies"`
	Dsh          struct {
		Profile struct {
			Bundles []string `json:"bundles"`
		} `json:"profile"`
	} `json:"dsh"`
}

func readManifest(t *testing.T, profileDir string) profileManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(profileDir, "package.json"))
	if err != nil {
		t.Fatalf("read profile manifest: %v", err)
	}
	var manifest profileManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse profile manifest: %v", err)
	}
	return manifest
}

func assertBundles(t *testing.T, manifest profileManifest, want ...string) {
	t.Helper()
	if len(manifest.Dsh.Profile.Bundles) != len(want) {
		t.Fatalf("bundles = %v, want %v", manifest.Dsh.Profile.Bundles, want)
	}
	for index, bundle := range want {
		if manifest.Dsh.Profile.Bundles[index] != bundle {
			t.Fatalf("bundles = %v, want %v", manifest.Dsh.Profile.Bundles, want)
		}
	}
}
