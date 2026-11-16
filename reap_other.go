//go:build !unix

package main

// @brief Do nothing: only Unix systems leave zombie processes (see reap_unix.go).
func reapZombies() {}
