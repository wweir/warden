//go:build !linux && !darwin && !windows

package install

import "fmt"

func InstallService(opts Options) error {
	_ = opts
	return fmt.Errorf("managed install is unsupported on this platform")
}

func managedConfigPath() string {
	return ""
}
