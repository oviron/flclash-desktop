//go:build !windows

// FlClash for Windows: the process in the notification area. It holds the
// engine and the model and starts the window when it is asked for.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "this is the Windows app; on macOS run FlClash.app")
	os.Exit(1)
}
