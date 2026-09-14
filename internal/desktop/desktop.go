package desktop

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed app.ico
var appIcon []byte

func NotifyError(title, text string) {
	notifyError(title, text)
}

func notifyStderr(title, text string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, text)
}
