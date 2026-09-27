package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckModsReportsBrokenFilesAndSharedTargets(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write := func(dir, name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(a, "0123456789abcdef.patch_0", "patch")
	write(b, "0123456789abcdef.patch_1", "")
	write(b, "fedcba9876543210.patch_0.stream", "orphan")
	mods := []*Mod{
		{Directory: a, Manifest: Manifest{Name: "A"}, Enabled: true},
		{Directory: b, Manifest: Manifest{Name: "B"}, Enabled: true},
	}
	findings := CheckMods(mods, nil)
	counts := map[string]int{}
	for _, finding := range findings {
		counts[finding.Kind]++
	}
	for _, kind := range []string{"Shared patch target", "Empty patch file", "Missing base patch"} {
		if counts[kind] != 1 {
			t.Fatalf("%s: %v", kind, findings)
		}
	}
	mods[1].Enabled = false
	for _, finding := range CheckMods(mods, nil) {
		if finding.Kind == "Shared patch target" {
			t.Fatal("disabled mod reported as active overlap")
		}
	}
	if _, err := os.Stat(filepath.Join(b, "0123456789abcdef.patch_1")); err != nil {
		t.Fatal("check modified files:", err)
	}
}
