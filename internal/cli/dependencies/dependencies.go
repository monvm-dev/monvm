// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Product describes an upstream release and its platform-specific asset naming rule.
type Product struct {
	Name       string
	Repository string
	Binary     string
	AssetName  func(version, architecture string) string
}

var (
	VictoriaMetrics = Product{Name: "VictoriaMetrics", Repository: "VictoriaMetrics/VictoriaMetrics", Binary: "victoria-metrics-prod", AssetName: victoriaAsset("victoria-metrics")}
	VictoriaLogs    = Product{Name: "VictoriaLogs", Repository: "VictoriaMetrics/VictoriaLogs", Binary: "victoria-logs-prod", AssetName: victoriaAsset("victoria-logs")}
	Envoy           = Product{Name: "Envoy", Repository: "envoyproxy/envoy", AssetName: envoyAsset}
	versionPattern  = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

// victoriaAsset returns the release archive naming rule for a Victoria product.
func victoriaAsset(prefix string) func(string, string) string {
	return func(version, architecture string) string {
		return fmt.Sprintf("%s-linux-%s-%s.tar.gz", prefix, architecture, version)
	}
}

// envoyAsset returns the Envoy executable asset name for an architecture.
func envoyAsset(version, architecture string) string {
	envoyArchitecture := "aarch_64"
	if architecture == "amd64" {
		envoyArchitecture = "x86_64"
	}
	return fmt.Sprintf("envoy-%s-linux-%s", strings.TrimPrefix(version, "v"), envoyArchitecture)
}

// Artifact describes a resolved, verified upstream release artifact.
type Artifact struct {
	Product   Product
	Version   string
	Name      string
	URL       string
	SHA256    string
	ObjectKey string
	Path      string
}

// release contains the subset of a GitHub release response used for resolution.
type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// Resolver resolves stable release assets through the GitHub API.
type Resolver struct {
	Client *http.Client
	API    string
}

// Resolve selects the exact open-source asset and trusted digest for a product release.
func (r Resolver) Resolve(ctx context.Context, product Product, version, architecture string) (Artifact, error) {
	if architecture != "amd64" && architecture != "arm64" {
		return Artifact{}, fmt.Errorf("unsupported architecture %q", architecture)
	}
	base := r.API
	if base == "" {
		base = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimSuffix(base, "/"), product.Repository)
	if version != "" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		if !versionPattern.MatchString(version) {
			return Artifact{}, fmt.Errorf("invalid %s version %q", product.Name, version)
		}
		endpoint = fmt.Sprintf("%s/repos/%s/releases/tags/%s", strings.TrimSuffix(base, "/"), product.Repository, version)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Artifact{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "monvm")
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return Artifact{}, fmt.Errorf("resolve %s release: %w", product.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Artifact{}, fmt.Errorf("resolve %s release: GitHub returned %s", product.Name, response.Status)
	}
	var found release
	if err = json.NewDecoder(response.Body).Decode(&found); err != nil {
		return Artifact{}, fmt.Errorf("decode %s release: %w", product.Name, err)
	}
	if found.Draft || found.Prerelease || !versionPattern.MatchString(found.Tag) {
		return Artifact{}, fmt.Errorf("%s release %q is not a stable release", product.Name, found.Tag)
	}
	if product.AssetName == nil {
		return Artifact{}, fmt.Errorf("%s has no release asset naming rule", product.Name)
	}
	expected := product.AssetName(found.Tag, architecture)
	for _, asset := range found.Assets {
		if asset.Name != expected {
			continue
		}
		digest, ok := strings.CutPrefix(asset.Digest, "sha256:")
		if !ok || len(digest) != 64 {
			return Artifact{}, fmt.Errorf("%s asset %s has no trusted SHA-256 digest", product.Name, expected)
		}
		if _, err = hex.DecodeString(digest); err != nil {
			return Artifact{}, fmt.Errorf("%s asset %s has an invalid SHA-256 digest", product.Name, expected)
		}
		return Artifact{Product: product, Version: found.Tag, Name: expected, URL: asset.URL, SHA256: digest, ObjectKey: "dependencies/" + expected}, nil
	}
	return Artifact{}, fmt.Errorf("%s release %s does not contain %s", product.Name, found.Tag, expected)
}

// Download retrieves an artifact into the cache and verifies its SHA-256 digest.
func Download(ctx context.Context, client *http.Client, cache string, artifact Artifact) (Artifact, error) {
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return artifact, err
	}
	artifact.Path = filepath.Join(cache, artifact.Name)
	if err := verify(artifact.Path, artifact.SHA256); err == nil {
		return artifact, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return artifact, err
	}
	request.Header.Set("User-Agent", "monvm")
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return artifact, fmt.Errorf("download %s: %w", artifact.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return artifact, fmt.Errorf("download %s: server returned %s", artifact.Name, response.Status)
	}
	file, err := os.CreateTemp(cache, ".download-*")
	if err != nil {
		return artifact, err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return artifact, copyErr
	}
	if closeErr != nil {
		return artifact, closeErr
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != artifact.SHA256 {
		return artifact, fmt.Errorf("verify %s: SHA-256 is %s, expected %s", artifact.Name, actual, artifact.SHA256)
	}
	if err = os.Chmod(temporary, 0o600); err != nil {
		return artifact, err
	}
	if err = os.Rename(temporary, artifact.Path); err != nil {
		return artifact, err
	}
	return artifact, nil
}

// verify checks that a cached file has the expected SHA-256 digest.
func verify(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("digest mismatch")
	}
	return nil
}
