//go:build !darwin

package daemon

import "fmt"

func triggerHelperAuthorization(_ string) error {
	return fmt.Errorf("collection-helper authorization not implemented on this platform")
}
