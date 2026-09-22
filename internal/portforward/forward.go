// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package portforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"time"
)

// Default ports and lifecycle timings for forwarding sessions.
const (
	// DefaultMetricsPort is the local and remote VictoriaMetrics UI port.
	DefaultMetricsPort = 18428
	// DefaultLogsPort is the local and remote VictoriaLogs UI port.
	DefaultLogsPort = 19428

	maximumPort     = 65535
	pollInterval    = 100 * time.Millisecond
	startupTimeout  = 30 * time.Second
	shutdownTimeout = 2 * time.Second
)

// Config identifies a deployment and its local forwarding ports.
type Config struct {
	Name        string
	Region      string
	Profile     string
	Mode        string
	Active      bool
	MetricsPort int
	LogsPort    int
}

// session owns one AWS Session Manager port-forwarding process.
type session struct {
	name       string
	instanceID string
	localPort  int
	remotePort int
	command    *exec.Cmd
	done       chan struct{}
	err        error
}

// Run forwards both service UIs until ctx is cancelled or a session fails.
func Run(ctx context.Context, config Config, output, stderr io.Writer) error {
	if err := checkPorts(config.MetricsPort, config.LogsPort); err != nil {
		return err
	}
	if !config.Active {
		return fmt.Errorf("deployment %q is torn down; run monvm setup %s first", config.Name, config.Name)
	}
	aws, err := findAWSCLI()
	if err != nil {
		return err
	}
	instances, err := findInstances(ctx, aws, config)
	if err != nil {
		return err
	}
	sessions := []*session{
		{name: "metrics", instanceID: instances.metrics, localPort: config.MetricsPort, remotePort: DefaultMetricsPort},
		{name: "logs", instanceID: instances.logs, localPort: config.LogsPort, remotePort: DefaultLogsPort},
	}
	if err = startSessions(aws, config, sessions, output, stderr); err != nil {
		return err
	}
	defer stopSessions(sessions)
	if err = waitUntilReady(ctx, sessions); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if _, err = fmt.Fprintf(output,
		"VictoriaMetrics: http://127.0.0.1:%d\nVictoriaLogs:    http://127.0.0.1:%d\nPress Ctrl-C to stop.\n",
		config.MetricsPort, config.LogsPort,
	); err != nil {
		return err
	}
	return waitUntilStopped(ctx, sessions)
}

// findAWSCLI locates the AWS CLI after verifying its Session Manager plugin.
func findAWSCLI() (string, error) {
	aws, err := exec.LookPath("aws")
	if err != nil {
		return "", errors.New("AWS CLI is required")
	}
	if _, err = exec.LookPath("session-manager-plugin"); err != nil {
		return "", errors.New("AWS Session Manager plugin is required")
	}
	return aws, nil
}

// checkPorts validates that distinct local ports are available on loopback.
func checkPorts(metrics, logs int) error {
	if metrics < 1 || metrics > maximumPort {
		return fmt.Errorf("invalid metrics port %d", metrics)
	}
	if logs < 1 || logs > maximumPort {
		return fmt.Errorf("invalid logs port %d", logs)
	}
	if metrics == logs {
		return errors.New("metrics and logs ports must be different")
	}
	ports := []struct {
		name string
		port int
	}{{"metrics", metrics}, {"logs", logs}}
	for _, candidate := range ports {
		listener, err := net.Listen("tcp", address(candidate.port))
		if err != nil {
			return fmt.Errorf("%s port %d is unavailable: %w", candidate.name, candidate.port, err)
		}
		if err = listener.Close(); err != nil {
			return err
		}
	}
	return nil
}

// startSessions starts every requested forwarding process or stops those already started.
func startSessions(aws string, config Config, sessions []*session, output, stderr io.Writer) error {
	for _, item := range sessions {
		if err := item.start(aws, config, output, stderr); err != nil {
			stopSessions(sessions)
			return err
		}
	}
	return nil
}

// start launches an AWS Session Manager process and begins observing its exit.
func (s *session) start(aws string, config Config, output, stderr io.Writer) error {
	s.command = exec.Command(aws, s.arguments(config)...)
	s.command.Stdout = output
	s.command.Stderr = stderr
	prepareProcess(s.command)
	if err := s.command.Start(); err != nil {
		return fmt.Errorf("start %s port forward: %w", s.name, err)
	}
	s.done = make(chan struct{})
	go func() {
		s.err = s.command.Wait()
		close(s.done)
	}()
	return nil
}

// arguments returns the AWS CLI arguments for one forwarding session.
func (s *session) arguments(config Config) []string {
	parameters := fmt.Sprintf("portNumber=[\"%d\"],localPortNumber=[\"%d\"]", s.remotePort, s.localPort)
	return awsArguments(config, "ssm", "start-session",
		"--target", s.instanceID,
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", parameters,
	)
}

// waitUntilReady waits until every local listener accepts connections.
func waitUntilReady(ctx context.Context, sessions []*session) error {
	deadline := time.NewTimer(startupTimeout)
	ticker := time.NewTicker(pollInterval)
	defer deadline.Stop()
	defer ticker.Stop()
	ready := make(map[int]bool, len(sessions))
	for len(ready) != len(sessions) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for port forwards")
		case <-ticker.C:
			if stopped := stoppedSession(sessions); stopped != nil {
				return fmt.Errorf("%s port forward stopped before becoming ready: %w", stopped.name, stopped.result())
			}
			probeSessions(sessions, ready)
		}
	}
	return nil
}

// probeSessions records each forwarding session whose local listener is ready.
func probeSessions(sessions []*session, ready map[int]bool) {
	for _, item := range sessions {
		if ready[item.localPort] {
			continue
		}
		connection, err := net.DialTimeout("tcp", address(item.localPort), pollInterval)
		if err == nil {
			ready[item.localPort] = true
			_ = connection.Close()
		}
	}
}

// waitUntilStopped blocks until cancellation or the unexpected exit of a session.
func waitUntilStopped(ctx context.Context, sessions []*session) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if stopped := stoppedSession(sessions); stopped != nil {
				return fmt.Errorf("%s port forward stopped: %w", stopped.name, stopped.result())
			}
		}
	}
}

// stoppedSession returns the first session that has exited, if any.
func stoppedSession(sessions []*session) *session {
	for _, item := range sessions {
		if item.done == nil {
			continue
		}
		select {
		case <-item.done:
			return item
		default:
		}
	}
	return nil
}

// result returns a useful error for an exited session.
func (s *session) result() error {
	if s.err != nil {
		return s.err
	}
	return errors.New("session exited")
}

// stopSessions interrupts running sessions and kills those that do not exit promptly.
func stopSessions(sessions []*session) {
	for _, item := range sessions {
		if item.running() {
			stopProcess(item.command)
		}
	}
	deadline := time.NewTimer(shutdownTimeout)
	defer deadline.Stop()
	for _, item := range sessions {
		if item.done == nil {
			continue
		}
		select {
		case <-item.done:
		case <-deadline.C:
			for _, running := range sessions {
				if running.running() {
					killProcess(running.command)
				}
			}
			return
		}
	}
}

// running reports whether a session process has started and not yet exited.
func (s *session) running() bool {
	if s.done == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// address returns the IPv4 loopback address for a local port.
func address(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}
