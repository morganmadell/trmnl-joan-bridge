//go:build unix

package main

import (
	"os/exec"
	"testing"
	"time"
)

func TestReapOnceReapsExitedChild(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a child process: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // let the child exit without being waited on
	if n := reapOnce(); n < 1 {
		t.Fatalf("reapOnce = %d, want at least 1", n)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("cmd.Wait succeeded, so the child was not reaped by reapOnce")
	}
	if n := reapOnce(); n != 0 {
		t.Errorf("second reapOnce = %d, want 0 (no children left)", n)
	}
}
