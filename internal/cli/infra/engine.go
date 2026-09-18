// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/monvm-dev/monvm/internal/cli/config"
	"github.com/monvm-dev/monvm/internal/cli/infra/aws"
)

// Run prepares and executes an OpenTofu/Terraform plan for a deployment.
func Run(ctx context.Context, variables Variables, destroy, autoApprove bool, input io.Reader, output, stderr io.Writer) error {
	command, err := SelectCommand()
	if err != nil {
		return err
	}
	directory, err := Prepare(variables)
	if err != nil {
		return err
	}
	environment := os.Environ()
	if os.Getenv("TF_PLUGIN_CACHE_DIR") == "" {
		cache, cacheErr := config.CacheDir()
		if cacheErr != nil {
			return cacheErr
		}
		pluginCache := filepath.Join(cache, "tf-plugins")
		if cacheErr = os.MkdirAll(pluginCache, 0o700); cacheErr != nil {
			return cacheErr
		}
		environment = append(environment, "TF_PLUGIN_CACHE_DIR="+pluginCache)
	}
	run := func(arguments ...string) error {
		process := exec.CommandContext(ctx, command, arguments...)
		process.Dir = directory
		process.Env = environment
		process.Stdin = input
		process.Stdout = output
		process.Stderr = stderr
		return process.Run()
	}
	initArguments := []string{"init", "-input=false", "-backend-config=bucket=" + variables.Bucket, "-backend-config=key=tfstate/monvm.tfstate", "-backend-config=region=" + variables.Region, "-backend-config=use_lockfile=true"}
	if variables.Profile != "" {
		initArguments = append(initArguments, "-backend-config=profile="+variables.Profile)
	}
	if err = run(initArguments...); err != nil {
		return fmt.Errorf("initialize OpenTofu/Terraform: %w", err)
	}
	planArguments := []string{"plan", "-detailed-exitcode", "-input=false", "-out=monvm.plan"}
	if destroy {
		planArguments = append(planArguments, "-destroy")
	}
	err = run(planArguments...)
	if err == nil {
		_, _ = fmt.Fprintln(output, "Infrastructure already matches the requested state.")
		return nil
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
		return fmt.Errorf("plan OpenTofu/Terraform: %w", err)
	}
	if !autoApprove {
		prompt := "Apply this OpenTofu/Terraform plan? [y/N] "
		if destroy {
			prompt = "Permanently destroy this MonVM deployment and its data? [y/N] "
		}
		if _, err = fmt.Fprint(output, prompt); err != nil {
			return err
		}
		answer, _ := bufio.NewReader(input).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			return errors.New("OpenTofu/Terraform apply cancelled")
		}
	}
	if err = run("apply", "-input=false", "monvm.plan"); err != nil {
		return fmt.Errorf("apply OpenTofu/Terraform plan: %w", err)
	}
	return nil
}

// Prepare writes a fresh embedded TF root module and its variables to the deployment work directory.
func Prepare(variables Variables) (string, error) {
	directory, err := WorkDir(variables.Name)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	for _, pattern := range []string{"*.tf", "*.tftpl", "*.sh", "*.plan", "*.auto.tfvars.json"} {
		paths, globErr := filepath.Glob(filepath.Join(directory, pattern))
		if globErr != nil {
			return "", globErr
		}
		for _, path := range paths {
			if err = os.Remove(path); err != nil {
				return "", err
			}
		}
	}
	entries, err := fs.ReadDir(aws.Module, ".")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		extension := filepath.Ext(entry.Name())
		if extension != ".tf" && extension != ".tftpl" && extension != ".sh" {
			continue
		}
		body, readErr := aws.Module.ReadFile(entry.Name())
		if readErr != nil {
			return "", readErr
		}
		if err = os.WriteFile(filepath.Join(directory, entry.Name()), body, 0o600); err != nil {
			return "", err
		}
	}
	body, err := json.MarshalIndent(variables, "", "  ")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(directory, "monvm.auto.tfvars.json"), append(body, '\n'), 0o600); err != nil {
		return "", err
	}
	return directory, nil
}

// WorkDir returns the cache directory containing one deployment's generated TF files.
func WorkDir(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid deployment name %q", name)
	}
	cache, err := config.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "infrastructure", name), nil
}

// Clean removes the generated TF files for a deployment.
func Clean(name string) error {
	directory, err := WorkDir(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(directory)
}
