package manager

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestImportRejectsDuplicateGUID(t *testing.T) {
	root := t.TempDir()
	settings := NewSettings("", root)
	modsDir := filepath.Join(root, "Mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"Version":1,"Guid":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","Name":"Existing","Options":[]}`)
	if err := os.WriteFile(filepath.Join(modsDir, "existing.json"), manifest, 0644); err != nil {
		_ = err
	}
	existingDir := filepath.Join(modsDir, "Existing")
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existingDir, "manifest.json"), manifest, 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "duplicate.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write(manifest)
	_ = zw.Close()
	_ = out.Close()
	if _, err := ImportArchive(settings, archive); err == nil {
		t.Fatal("expected duplicate GUID rejection")
	}
}

func TestV1OptionsMultiSelectRoundTrip(t *testing.T) {
	for _, key := range []string{"Version", "version"} {
		t.Run(key, func(t *testing.T) {
			m, err := parseManifest([]byte(`{"` + key + `":1,"Name":"Pack","Options":[{"Name":"A","Description":"First feature","Include":["a"]},{"Name":"B","Description":"Second feature","Include":["b"]},{"Name":"C","Include":["c"]}]}`))
			if err != nil {
				t.Fatal(err)
			}
			if m.Version != 1 || m.Options[0].Description != "First feature" {
				t.Fatalf("incorrect manifest: %+v", m)
			}
			root := t.TempDir()
			mod := &Mod{Directory: root, Manifest: *m, Enabled: true, EnabledOptions: []bool{true, false, true}, SelectedOptions: make([]int, 3)}
			s := NewSettings("", root)
			if err := SaveProfile(s, []*Mod{mod}); err != nil {
				t.Fatal(err)
			}
			mod.EnabledOptions = []bool{false, true, false}
			if err := LoadProfile(s, []*Mod{mod}); err != nil {
				t.Fatal(err)
			}
			dirs := includeDirs(mod)
			if len(dirs) != 2 || dirs[0] != filepath.Join(root, "a") || dirs[1] != filepath.Join(root, "c") {
				t.Fatalf("wrong selected directories: %v", dirs)
			}
		})
	}
}

func TestLegacyStringOptions(t *testing.T) {
	m, err := parseManifest([]byte(`{"Name":"Legacy","Options":["a","b"]}`))
	if err != nil || m.Version != -1 || len(m.Options) != 2 {
		t.Fatalf("legacy parse: %+v, %v", m, err)
	}
}

func TestInstalledMegapackManifest(t *testing.T) {
	data, err := os.ReadFile("../../Mods/Vanilla-Plus-Megapack-Rows-v29/manifest.json")
	if os.IsNotExist(err) {
		t.Skip("local mod fixture unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || len(m.Options) != 13 {
		t.Fatalf("unexpected pack: version=%d options=%d", m.Version, len(m.Options))
	}
	for _, option := range m.Options {
		if option.Description == "" {
			t.Errorf("missing description for %s", option.Name)
		}
	}
}
