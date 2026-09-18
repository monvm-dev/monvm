// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// TestStoreLifecycle verifies atomic persistence, retrieval, and deletion of deployment metadata.
func TestStoreLifecycle(t *testing.T) {
	store := &Store{Directory: t.TempDir()}
	want := Deployment{Name: "demo", Region: "us-east-1", Bucket: "monvm-demo", Variables: []byte(`{"active":true}`)}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	var gotVariables, wantVariables map[string]any
	if err = json.Unmarshal(got.Variables, &gotVariables); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(want.Variables, &wantVariables); err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.Region != want.Region || got.Bucket != want.Bucket || !reflect.DeepEqual(gotVariables, wantVariables) {
		t.Fatalf("loaded deployment = %#v, want %#v", got, want)
	}
	if err = store.Delete("demo"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load("demo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("load after delete = %v, want ErrNotFound", err)
	}
}
