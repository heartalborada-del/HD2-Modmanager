//go:build gui && !windows

package main

import (
	"github.com/sqweek/dialog"
)

func selectArchives(startDir string) ([]string, error) {
	p, err := dialog.File().Title("Select mod archive").SetStartDir(startDir).Filter("Mod archives", "zip", "7z", "rar").Load()
	if err != nil {
		return nil, err
	}
	return []string{p}, nil
}
