// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monvm-dev/monvm/internal/cli/config"
	"github.com/monvm-dev/monvm/internal/cli/infra"
)

// loadDeployment reads local metadata and verifies its identifying fields.
func loadDeployment(name string) (*config.Store, infra.Variables, error) {
	store, err := config.DefaultStore()
	if err != nil {
		return nil, infra.Variables{}, err
	}
	deployment, err := store.Load(name)
	if err != nil {
		return nil, infra.Variables{}, err
	}
	var variables infra.Variables
	if err = json.Unmarshal(deployment.Variables, &variables); err != nil {
		return nil, infra.Variables{}, fmt.Errorf("read deployment variables: %w", err)
	}
	if variables.Name != name || variables.Region != deployment.Region || variables.Bucket != deployment.Bucket {
		return nil, infra.Variables{}, errors.New("local deployment metadata is inconsistent")
	}
	return store, variables, nil
}

// deploymentBody serializes module variables into local and remote deployment metadata.
func deploymentBody(variables infra.Variables) (config.Deployment, []byte, error) {
	encoded, err := json.Marshal(variables)
	if err != nil {
		return config.Deployment{}, nil, err
	}
	deployment := config.Deployment{Name: variables.Name, Region: variables.Region, Profile: variables.Profile, Bucket: variables.Bucket, Variables: encoded}
	body, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return config.Deployment{}, nil, err
	}
	return deployment, append(body, '\n'), nil
}
