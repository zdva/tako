//go:build !linux && !darwin && !windows

package secrets

import "errors"

// platformBinding reports that this OS has no machine id Tako knows how to
// read. The secret store then keeps a random key in a 0600 file.
func platformBinding() ([]byte, error) {
	return nil, errors.ErrUnsupported
}
