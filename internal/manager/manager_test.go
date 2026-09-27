package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfileAndDeployTriplet(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "game")
	storage := filepath.Join(root, "store")
	modDir := filepath.Join(storage, "Mods", "Example")
	patchDir := modDir
	if err := os.MkdirAll(filepath.Join(game, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"Version":1,"Guid":"11111111-1111-1111-1111-111111111111","Name":"Example","Description":"test"}`
	if err := os.WriteFile(filepath.Join(modDir, "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"abcdef0123456789.patch_4", "abcdef0123456789.patch_4.gpu_resources", "abcdef0123456789.patch_4.stream"} {
		if err := os.WriteFile(filepath.Join(patchDir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewSettings(game, storage)
	mods, _, err := Load(s)
	if err != nil || len(mods) != 1 {
		t.Fatalf("load: %v (%d)", err, len(mods))
	}
	mods[0].Enabled = false
	if err := SaveProfile(s, mods); err != nil {
		t.Fatal(err)
	}
	mods[0].Enabled = true
	if err := Deploy(s, mods); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"abcdef0123456789.patch_0", "abcdef0123456789.patch_0.gpu_resources", "abcdef0123456789.patch_0.stream"} {
		if _, err := os.Stat(filepath.Join(game, "data", name)); err != nil {
			t.Errorf("missing deployed %s: %v", name, err)
		}
	}
	if err := LoadProfile(s, mods); err != nil {
		t.Fatal(err)
	}
	if mods[0].Enabled {
		t.Error("profile state was not restored")
	}
}
