// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestResolveSelectsExactOSSAsset verifies that enterprise and malformed variants are ignored.
func TestResolveSelectsExactOSSAsset(t *testing.T) {
	digest := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/VictoriaMetrics/VictoriaLogs/releases/latest" {
			t.Errorf("unexpected path %q", request.URL.Path)
		}
		_, _ = fmt.Fprintf(writer, `{"tag_name":"v1.52.0","assets":[
{"name":"victoria-logs-enterprise-linux-arm64-v1.52.0.tar.gz","browser_download_url":"wrong","digest":"sha256:%s"},
{"name":"victoria-logs-linux-arm64-v1.52.0.tar.gz","browser_download_url":"https://example.test/logs","digest":"sha256:%s"}]}`, digest, digest)
	}))
	defer server.Close()

	artifact, err := (Resolver{Client: server.Client(), API: server.URL}).Resolve(context.Background(), VictoriaLogs, "", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Name != "victoria-logs-linux-arm64-v1.52.0.tar.gz" || artifact.URL != "https://example.test/logs" || artifact.SHA256 != digest {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
}

// TestResolveSelectsExactEnvoyAsset verifies exact version and architecture selection.
func TestResolveSelectsExactEnvoyAsset(t *testing.T) {
	digest := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/envoyproxy/envoy/releases/tags/v1.39.1" {
			t.Errorf("unexpected path %q", request.URL.Path)
		}
		_, _ = fmt.Fprintf(writer, `{"tag_name":"v1.39.1","assets":[
{"name":"envoy-1.39.1-linux-aarch_64-contrib","browser_download_url":"wrong-contrib","digest":"sha256:%s"},
{"name":"envoy-1.39.1-linux-x86_64","browser_download_url":"wrong-architecture","digest":"sha256:%s"},
{"name":"envoy-1.39.1-linux-aarch_64","browser_download_url":"https://example.test/envoy","digest":"sha256:%s"}]}`, digest, digest, digest)
	}))
	defer server.Close()

	artifact, err := (Resolver{Client: server.Client(), API: server.URL}).Resolve(context.Background(), Envoy, "1.39.1", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Name != "envoy-1.39.1-linux-aarch_64" || artifact.URL != "https://example.test/envoy" || artifact.SHA256 != digest {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
}

// TestResolveRejectsMissingDigest verifies that unsigned release metadata is rejected.
func TestResolveRejectsMissingDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"tag_name":"v1.52.0","assets":[{"name":"victoria-logs-linux-amd64-v1.52.0.tar.gz","browser_download_url":"https://example.test/logs"}]}`))
	}))
	defer server.Close()
	_, err := (Resolver{Client: server.Client(), API: server.URL}).Resolve(context.Background(), VictoriaLogs, "1.52.0", "amd64")
	if err == nil || !strings.Contains(err.Error(), "trusted SHA-256") {
		t.Fatalf("expected missing digest error, got %v", err)
	}
}

// TestDownloadVerifiesContentAndUsesCache verifies digest checking and cache reuse.
func TestDownloadVerifiesContentAndUsesCache(t *testing.T) {
	body := []byte("verified archive")
	hash := sha256.Sum256(body)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	artifact := Artifact{Name: "release.tar.gz", URL: server.URL, SHA256: hex.EncodeToString(hash[:])}

	first, err := Download(context.Background(), server.Client(), t.TempDir(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(first.Path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("downloaded content = %q, %v", got, err)
	}
	if _, err = Download(context.Background(), server.Client(), first.Path+"-cache", artifact); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 for distinct caches", requests)
	}
	if _, err = Download(context.Background(), server.Client(), first.Path+"-cache", artifact); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("cached download made another request; requests = %d", requests)
	}
}
