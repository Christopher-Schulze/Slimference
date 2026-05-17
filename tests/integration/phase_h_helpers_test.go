//go:build integration

package integration_test

import "os"

func mkdirAll(path string, mode uint32) error {
	return os.MkdirAll(path, os.FileMode(mode))
}
