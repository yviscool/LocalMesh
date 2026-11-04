//go:build !windows

package main

// uiPipeAddress is the student UI's unix socket path on non-Windows.
const uiPipeAddress = "/tmp/localmesh-student-ui.sock"
