// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestClientCAStoreLifecycle verifies enrollment, rotation, revocation, and fail-closed behavior.
func TestClientCAStoreLifecycle(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	objects := filepath.Join(t.TempDir(), "objects")
	fakeBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(filepath.Join(objects, "trust"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(objects, "trust", "cluster-a.pem")
	deny := filepath.Join(state, "deny-all.pem")
	createTestCA(t, valid, "valid source")
	createTestCA(t, deny, "deny all")

	listing := filepath.Join(t.TempDir(), "listing.json")
	writeFile(t, listing, `{"IsTruncated":false,"Contents":[{"Key":"trust/cluster-a.pem"},{"Key":"trust/INVALID.pem"},{"Key":"trust/cluster-a/ignored.pem"}]}`)
	writeFile(t, filepath.Join(fakeBin, "aws"), `#!/usr/bin/env bash
set -eu
case "$2" in
  list-objects-v2) cat "$FAKE_LIST" ;;
  get-object)
    key=$6
    destination=$7
    if [[ -f "$FAKE_OBJECTS/$key" ]]; then
      cp "$FAKE_OBJECTS/$key" "$destination"
      printf '{}\n'
    else
      echo 'An error occurred (NoSuchKey) when calling GetObject' >&2
      exit 255
    fi
    ;;
  *) echo "unsupported fake AWS command: $*" >&2; exit 2 ;;
esac
`)
	if err := os.Chmod(filepath.Join(fakeBin, "aws"), 0o755); err != nil {
		t.Fatal(err)
	}
	refreshWrapper := filepath.Join(fakeBin, "refresh")
	writeFile(t, refreshWrapper, "#!/usr/bin/env bash\nexec bash "+shellQuote(filepath.Join(mustWorkingDirectory(t), "trust-refresh.sh"))+"\n")
	if err := os.Chmod(refreshWrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(t.TempDir(), "trust.env")
	writeFile(t, environment, "MONVM_BUCKET=test\nMONVM_REGION=us-east-1\nMONVM_S3_ENDPOINT=https://example.test\n")
	env := append(os.Environ(),
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
		"FAKE_LIST="+listing,
		"FAKE_OBJECTS="+objects,
		"MONVM_TRUST_ENV="+environment,
		"MONVM_TRUST_STATE="+state,
		"MONVM_TRUST_REFRESH="+refreshWrapper,
	)

	runTrustScript(t, "trust-enroll.sh", env)
	if got := strings.TrimSpace(readFile(t, filepath.Join(state, "known-keys"))); got != "trust/cluster-a.pem" {
		t.Fatalf("enrolled keys = %q", got)
	}
	original := readFile(t, filepath.Join(state, "sources", "cluster-a.pem"))

	writeFile(t, valid, "not a certificate")
	runTrustScript(t, "trust-refresh.sh", env)
	if got := readFile(t, filepath.Join(state, "sources", "cluster-a.pem")); got != original {
		t.Fatal("malformed update replaced the last-known-good bundle")
	}

	if err := os.Remove(valid); err != nil {
		t.Fatal(err)
	}
	runTrustScript(t, "trust-refresh.sh", env)
	if _, err := os.Stat(filepath.Join(state, "sources", "cluster-a.pem")); !os.IsNotExist(err) {
		t.Fatalf("missing trust object did not revoke its source: %v", err)
	}
	if got, want := readFile(t, filepath.Join(state, "bundle.pem")), readFile(t, deny); got != want {
		t.Fatal("empty trust set did not install the deny-all authority")
	}

	createTestCA(t, valid, "rotated source")
	writeFile(t, listing, `{"IsTruncated":true,"Contents":[{"Key":"trust/new-source.pem"}]}`)
	runTrustScript(t, "trust-enroll.sh", env)
	if got := strings.TrimSpace(readFile(t, filepath.Join(state, "known-keys"))); got != "trust/cluster-a.pem" {
		t.Fatalf("truncated listing changed enrollment to %q", got)
	}
	if got := readFile(t, filepath.Join(state, "sources", "cluster-a.pem")); got == original {
		t.Fatal("valid certificate rotation was not refreshed")
	}
}

// createTestCA writes a short-lived CA certificate for trust-store tests.
func createTestCA(t *testing.T, destination, commonName string) {
	t.Helper()
	key := destination + ".key"
	command := exec.Command("openssl", "req", "-x509", "-newkey", "ed25519", "-nodes", "-days", "1",
		"-subj", "/CN="+commonName, "-addext", "basicConstraints=critical,CA:TRUE",
		"-addext", "keyUsage=critical,keyCertSign,cRLSign", "-keyout", key, "-out", destination)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create test CA: %v\n%s", err, output)
	}
	_ = os.Remove(key)
}

// runTrustScript executes an embedded trust script with an isolated environment.
func runTrustScript(t *testing.T, name string, environment []string) {
	t.Helper()
	command := exec.Command("bash", filepath.Join(mustWorkingDirectory(t), name))
	command.Env = environment
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run %s: %v\n%s", name, err, output)
	}
}

// mustWorkingDirectory returns the package test directory or fails the test.
func mustWorkingDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

// writeFile writes a test fixture or fails the test.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// readFile reads a test fixture or fails the test.
func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// shellQuote returns a single shell-safe argument.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
