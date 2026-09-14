//go:build !windows

package desktop

import "fmt"

func Supported() bool { return false }

func Open(url, dataDir string) error {
	return fmt.Errorf("desktop window is only supported on windows")
}

func notifyError(title, text string) {
	notifyStderr(title, text)
}
