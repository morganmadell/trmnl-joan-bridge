//go:build unix

package main

import (
	"syscall"
	"time"
)

// @brief Reap every child process that has already exited.
//
// @details wait4 with WNOHANG only reaps processes that have already exited, so it cannot affect a
// process that is still running.
//
// @return Number of children reaped.
func reapOnce() int {
	n := 0
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			return n
		}
		n++
	}
}

// @brief Reap exited child processes every 30 seconds, forever.
//
// @details chromedp does not always reap a Chromium that failed to start, and each failed render
// leaks one zombie. Over days of failed renders the zombies fill the host's process table, which
// happened from 2026-09-24 to 2026-10-01 (about 19,000 zombies) and broke docker exec and new
// container starts, not only rendering.
func reapZombies() {
	for {
		time.Sleep(30 * time.Second)
		reapOnce()
	}
}
