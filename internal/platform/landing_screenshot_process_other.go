//go:build !unix

package platform

import (
	"os"
	"os/exec"
)

func configureScreenshotProcess(cmd *exec.Cmd) {}

func killScreenshotProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}
