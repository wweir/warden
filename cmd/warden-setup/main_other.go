//go:build !windows

package main

import (
	"fmt"
	"os"
)

var Version, BuildTime string

func main() {
	fmt.Fprintln(os.Stderr, "warden-setup is only supported on Windows")
	os.Exit(1)
}
