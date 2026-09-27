//go:build gui && windows

package main

import (
	"errors"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const ofnAllowMultiSelect = 0x00000200
const ofnExplorer = 0x00080000

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

var getOpenFileName = syscall.NewLazyDLL("comdlg32.dll").NewProc("GetOpenFileNameW")

func selectArchives(startDir string) ([]string, error) {
	buf := make([]uint16, 65536)
	// UTF16FromString rejects embedded NULs; Windows filters require them.
	filter := append(utf16.Encode([]rune("Mod archives (*.zip;*.7z;*.rar)")), 0)
	filter = append(filter, utf16.Encode([]rune("*.zip;*.7z;*.rar"))...)
	filter = append(filter, 0)
	filter = append(filter, utf16.Encode([]rune("All files (*.*)"))...)
	filter = append(filter, 0)
	filter = append(filter, utf16.Encode([]rune("*.*"))...)
	filter = append(filter, 0, 0)
	initial, _ := syscall.UTF16PtrFromString(startDir)
	of := openFileName{lStructSize: uint32(unsafe.Sizeof(openFileName{})), lpstrFilter: &filter[0], nFilterIndex: 1, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)), lpstrInitialDir: initial, flags: ofnAllowMultiSelect | ofnExplorer}
	r, _, _ := getOpenFileName.Call(uintptr(unsafe.Pointer(&of)))
	if r == 0 {
		return nil, errors.New("file picker cancelled")
	}
	values := make([]string, 0, 8)
	for i := 0; i < len(buf); {
		if buf[i] == 0 {
			break
		}
		j := i
		for j < len(buf) && buf[j] != 0 {
			j++
		}
		values = append(values, syscall.UTF16ToString(buf[i:j]))
		i = j + 1
	}
	if len(values) == 0 {
		return nil, errors.New("no archive selected")
	}
	if len(values) == 1 {
		return values, nil
	}
	dir := values[0]
	for i := 1; i < len(values); i++ {
		values[i] = filepath.Join(dir, values[i])
	}
	return values[1:], nil
}
