//go:build !unix

package codeagents

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
