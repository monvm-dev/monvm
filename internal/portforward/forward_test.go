// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package portforward

import (
	"net"
	"testing"
)

// TestCheckPortsRejectsInvalidAndDuplicatePorts verifies local endpoint validation.
func TestCheckPortsRejectsInvalidAndDuplicatePorts(t *testing.T) {
	for _, ports := range [][2]int{{0, DefaultLogsPort}, {DefaultMetricsPort, maximumPort + 1}, {DefaultMetricsPort, DefaultMetricsPort}} {
		if err := checkPorts(ports[0], ports[1]); err == nil {
			t.Errorf("checkPorts(%d, %d) succeeded", ports[0], ports[1])
		}
	}
}

// TestCheckPortsRejectsOccupiedPort verifies an existing local listener is not displaced.
func TestCheckPortsRejectsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port
	if err = checkPorts(port, DefaultLogsPort); err == nil {
		t.Fatal("expected occupied port to be rejected")
	}
}

// TestAWSArguments verifies deployment credentials precede service arguments.
func TestAWSArguments(t *testing.T) {
	config := Config{Region: "us-west-2", Profile: "production"}
	got := awsArguments(config, "ssm", "start-session")
	want := []string{"--region", "us-west-2", "--profile", "production", "ssm", "start-session"}
	assertArguments(t, got, want)
}

// TestSessionArguments verifies the SSM document receives distinct remote and local ports.
func TestSessionArguments(t *testing.T) {
	item := session{instanceID: "i-0123456789", localPort: 20000, remotePort: DefaultLogsPort}
	got := item.arguments(Config{Region: "us-west-2"})
	want := []string{
		"--region", "us-west-2", "ssm", "start-session",
		"--target", "i-0123456789",
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", `portNumber=["19428"],localPortNumber=["20000"]`,
	}
	assertArguments(t, got, want)
}

// assertArguments compares complete argument lists.
func assertArguments(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("arguments = %q", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("arguments = %q, want %q", got, want)
		}
	}
}
