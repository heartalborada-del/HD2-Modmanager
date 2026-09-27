//go:build !gui

package main

import (
	"errors"
	"hd2modmanager/internal/manager"
)

func runGUI(manager.Settings) error {
	return errors.New("GUI requires: go run -tags gui ./cmd/hd2mm -gui")
}
