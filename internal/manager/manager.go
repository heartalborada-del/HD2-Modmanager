package manager

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	sevenzip "github.com/bodgit/sevenzip"
	rardecode "github.com/nwaples/rardecode/v2"
)

type Settings struct {
	GameDir, StorageDir, TempDir string
	SkipList                     map[string]bool
	Pinned                       map[string]bool
}
type Manifest struct {
	Version     int      `json:"-"`
	GUID        string   `json:"guid"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IconPath    string   `json:"iconPath,omitempty"`
	Options     []Option `json:"options,omitempty"`
}
type Option struct {
	Description string      `json:"description,omitempty"`
	Name        string      `json:"name,omitempty"`
	Include     []string    `json:"include,omitempty"`
	Image       string      `json:"image,omitempty"`
	SubOptions  []SubOption `json:"subOptions,omitempty"`
}
type SubOption struct {
	Description string   `json:"description,omitempty"`
	Name        string   `json:"name,omitempty"`
	Include     []string `json:"include"`
	Image       string   `json:"image,omitempty"`
}
type Mod struct {
	Directory       string
	Manifest        Manifest
	Enabled         bool
	EnabledOptions  []bool
	SelectedOptions []int
	ArchivePath     string
}
type Problem struct{ Directory, Kind, Detail string }
type ProfileEntry struct {
	GUID     string `json:"guid"`
	Enabled  bool   `json:"enabled"`
	Toggled  []bool `json:"toggled"`
	Selected []int  `json:"selected"`
}

var patchRE = regexp.MustCompile(`^([a-z0-9]{16})\.patch_([0-9]+)(\.(?:stream|gpu_resources))?$`)

func configDir(storage string) string { return filepath.Join(storage, "config") }

func readConfig(storage, name string) ([]byte, error) {
	path := filepath.Join(configDir(storage), name)
	legacy := filepath.Join(storage, name)
	b, err := os.ReadFile(path)
	legacyBytes, legacyErr := os.ReadFile(legacy)
	if legacyErr == nil {
		useLegacy := os.IsNotExist(err)
		if !useLegacy {
			if current, statErr := os.Stat(path); statErr == nil {
				if old, oldErr := os.Stat(legacy); oldErr == nil {
					useLegacy = old.ModTime().After(current.ModTime())
				}
			}
		}
		if useLegacy && os.MkdirAll(configDir(storage), 0755) == nil {
			if writeErr := os.WriteFile(path, legacyBytes, 0644); writeErr == nil {
				b, err = legacyBytes, nil
			}
		}
		if err == nil {
			_ = os.Remove(legacy)
		}
	}
	return b, err
}

func NewSettings(game, storage string) Settings {
	return Settings{GameDir: game, StorageDir: storage, TempDir: filepath.Join(storage, "Temp"), SkipList: map[string]bool{}, Pinned: map[string]bool{}}
}

func CleanupTemp(settings Settings) error {
	if settings.TempDir == "" {
		return nil
	}
	return os.RemoveAll(settings.TempDir)
}

// LaunchGame starts Helldivers 2 through Steam.
func LaunchGame(settings Settings) error {
	cmd, err := launchGameCommand(settings)
	if err != nil {
		return err
	}
	return cmd.Start()
}

func launchGameCommand(settings Settings) (*exec.Cmd, error) {
	for _, exe := range []string{
		filepath.Join(settings.GameDir, "bin", "helldivers2.exe"),
		filepath.Join(settings.GameDir, "helldivers2.exe"),
	} {
		if info, err := os.Stat(exe); err == nil && !info.IsDir() {
			cmd := exec.Command(exe)
			cmd.Dir = filepath.Dir(exe)
			hideProcessWindow(cmd)
			return cmd, nil
		}
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "start", "", "steam://run/553850")
		hideProcessWindow(cmd)
		return cmd, nil
	}
	cmd := exec.Command("steam", "-applaunch", "553850")
	hideProcessWindow(cmd)
	return cmd, nil
}

// WaitForGameStartup watches the Windows game process after launching Steam.
// It reports whether the process appeared and then exited during startup.
func WaitForGameStartup(settings Settings, waitSeconds int) (started, crashed bool, err error) {
	if err = LaunchGame(settings); err != nil {
		return false, false, err
	}
	if waitSeconds < 10 {
		waitSeconds = 10
	}
	for i := 0; i < waitSeconds*2; i++ {
		running := gameProcessRunning()
		if running {
			started = true
			// A process that exits within the first ten seconds is a likely
			// startup crash. Do not close or interfere with a running game.
			for j := 0; j < 20; j++ {
				time.Sleep(500 * time.Millisecond)
				if !gameProcessRunning() {
					return true, true, nil
				}
			}
			return true, false, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false, false, nil
}

// BinaryStartupTest recursively narrows an enabled-mod set. A group is
// considered faulty only when the game process exits before thresholdSeconds.
type BinaryTestStep struct {
	Names   []string
	Crashed bool
}

func BinaryStartupTest(settings Settings, mods []*Mod, thresholdSeconds int, progress func(string), report func(BinaryTestStep)) ([]string, error) {
	active := make([]*Mod, 0, len(mods))
	for _, mod := range mods {
		if mod.Enabled {
			active = append(active, mod)
		}
	}
	if thresholdSeconds < 10 {
		thresholdSeconds = 10
	}
	if len(active) == 0 {
		return nil, nil
	}
	var test func([]*Mod, bool) ([]*Mod, error)
	test = func(group []*Mod, knownBad bool) ([]*Mod, error) {
		if progress != nil {
			progress(fmt.Sprintf("Testing %d mods", len(group)))
		}
		if !knownBad {
			if err := Deploy(settings, group); err != nil {
				return nil, err
			}
			crashed, err := startupCrashed(settings, thresholdSeconds)
			if err != nil {
				return nil, err
			}
			if !crashed {
				if report != nil {
					report(BinaryTestStep{Names: modNames(group), Crashed: false})
				}
				return nil, nil
			}
		}
		if len(group) <= 1 {
			if report != nil {
				report(BinaryTestStep{Names: modNames(group), Crashed: true})
			}
			return group, nil
		}
		mid := len(group) / 2
		if left, err := test(group[:mid], false); err != nil {
			return nil, err
		} else if len(left) > 0 {
			return left, nil
		}
		// The complete group was already known to fail and the left half just
		// passed, so the right half is known-bad without another full test.
		return test(group[mid:], true)
	}
	found, err := test(active, false)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(found))
	for i, mod := range found {
		names[i] = mod.Manifest.Name
	}
	return names, nil
}

func modNames(mods []*Mod) []string {
	names := make([]string, len(mods))
	for i, mod := range mods {
		names[i] = mod.Manifest.Name
	}
	return names
}

func startupCrashed(settings Settings, thresholdSeconds int) (bool, error) {
	if gameProcessRunning() {
		killGameProcess()
		// Give the game and its crash handler a moment to release files before
		// deploying the next binary-test subset.
		for i := 0; i < 20 && gameProcessRunning(); i++ {
			time.Sleep(250 * time.Millisecond)
		}
	}
	cmd, err := launchGameCommand(settings)
	if err != nil {
		return false, err
	}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	seen := false
	for i := 0; i < thresholdSeconds*2; i++ {
		if gameProcessRunning() {
			seen = true
		}
		// Ignore stale dialogs from a previous run until this run has
		// actually produced an HD2 process.
		if seen && gameFatalDialogVisible() {
			killProcessTree(cmd)
			killGameProcess()
			return true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if runtime.GOOS == "windows" {
		killProcessTree(cmd)
		killGameProcess()
	} else if seen {
		killProcessTree(cmd)
	}
	return false, nil
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		pid := strconv.Itoa(cmd.Process.Pid)
		kill := exec.Command("taskkill", "/F", "/T", "/PID", pid)
		hideProcessWindow(kill)
		_ = kill.Run()
	} else {
		_ = cmd.Process.Kill()
	}
}

func killGameProcess() {
	if runtime.GOOS != "windows" {
		return
	}
	for _, image := range []string{"helldivers2.exe", "crs-handler.exe", "helldivers2_crash_handler.exe", "crashreportclient.exe", "nProtectGameGuard.exe", "npggsvc.exe", "npgmup.exe", "GameMon.des"} {
		cmd := exec.Command("taskkill", "/F", "/T", "/IM", image)
		hideProcessWindow(cmd)
		_ = cmd.Run()
	}
	// The fatal dialog may be owned by a helper process rather than the game
	// image. taskkill can close it by its native window title as well.
	for _, title := range []string{"Fatal Error!", "Fatal Error"} {
		cmd := exec.Command("taskkill", "/F", "/T", "/FI", "WINDOWTITLE eq "+title)
		hideProcessWindow(cmd)
		_ = cmd.Run()
	}
}

func gameProcessRunning() bool {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq helldivers2.exe", "/NH")
		hideProcessWindow(cmd)
		out, err := cmd.Output()
		text := strings.ToLower(string(out))
		return err == nil && (strings.Contains(text, "helldivers2.exe") || strings.Contains(text, "crs-handler.exe"))
	}
	out, err := exec.Command("pgrep", "-x", "helldivers2").Output()
	return err == nil && len(out) > 0
}

func gameFatalDialogVisible() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cmd := exec.Command("tasklist", "/V", "/FI", "IMAGENAME eq helldivers2.exe", "/FO", "CSV", "/NH")
	hideProcessWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	text := strings.ToLower(string(out))
	for _, line := range strings.Split(text, "\n") {
		isHD2 := strings.Contains(line, "helldivers2.exe") || strings.Contains(line, "crs-handler.exe") || strings.Contains(line, "crashreportclient.exe")
		if isHD2 && (strings.Contains(line, "fatal error") || strings.Contains(line, "unexpected error while loading game files") || strings.Contains(line, "0x44415441")) {
			return true
		}
	}
	return false
}

// FindGameDirectory discovers the usual Steam install locations without
// requiring a registry dependency. It also reads Steam's libraryfolders.vdf,
// so secondary drives are supported.
func FindGameDirectory() string {
	const gameName = "Helldivers 2"
	candidates := []string{}
	add := func(p string) {
		if p != "" {
			candidates = append(candidates, p)
		}
	}
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		add(filepath.Join(root, "Steam", "steamapps", "common", gameName))
		add(filepath.Join(root, "SteamLibrary", "steamapps", "common", gameName))
	}
	for drive := 'A'; drive <= 'Z'; drive++ {
		root := fmt.Sprintf("%c:\\", drive)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		add(filepath.Join(root, "Steam", "steamapps", "common", gameName))
		add(filepath.Join(root, "SteamLibrary", "steamapps", "common", gameName))
		add(filepath.Join(root, "Games", "SteamLibrary", "steamapps", "common", gameName))
		vdf := filepath.Join(root, "Steam", "steamapps", "libraryfolders.vdf")
		if b, err := os.ReadFile(vdf); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				parts := strings.Split(line, "\"")
				if len(parts) >= 4 && strings.TrimSpace(parts[1]) == "path" {
					add(filepath.Join(strings.ReplaceAll(parts[3], "\\\\", "\\"), "steamapps", "common", gameName))
				}
			}
		}
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return filepath.Clean(p)
		}
	}
	return ""
}

func Load(settings Settings) ([]*Mod, []Problem, error) {
	if settings.Pinned == nil {
		settings.Pinned = map[string]bool{}
	}
	if pinned, err := readConfig(settings.StorageDir, "pinned.json"); err == nil {
		var ids []string
		if json.Unmarshal(pinned, &ids) == nil {
			for _, id := range ids {
				settings.Pinned[id] = true
			}
		}
	}
	var order []string
	if data, err := readConfig(settings.StorageDir, "order.json"); err == nil {
		_ = json.Unmarshal(data, &order)
	}
	root := filepath.Join(settings.StorageDir, "Mods")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, nil, err
	}
	var mods []*Mod
	var problems []Problem
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if e.IsDir() {
			manifestPath := filepath.Join(path, "manifest.json")
			data, readErr := os.ReadFile(manifestPath)
			if os.IsNotExist(readErr) {
				if _, markerErr := os.Stat(filepath.Join(path, ".category")); markerErr == nil {
					return nil // marked category folder; continue walking into it
				}
				return filepath.SkipDir
			}
			if readErr != nil {
				problems = append(problems, Problem{path, "no_manifest", readErr.Error()})
				return filepath.SkipDir
			}
			m, parseErr := parseManifest(data)
			if parseErr != nil {
				problems = append(problems, Problem{path, "invalid_manifest", parseErr.Error()})
				return filepath.SkipDir
			}
			if m.GUID == "" {
				m.GUID = stableGUID(filepath.Base(path))
			}
			if seen[m.GUID] {
				problems = append(problems, Problem{path, "duplicate", m.GUID})
				return filepath.SkipDir
			}
			seen[m.GUID] = true
			for _, o := range m.Options {
				for _, p := range o.Include {
					if _, statErr := os.Stat(filepath.Join(path, p)); statErr != nil {
						problems = append(problems, Problem{path, "invalid_path", p})
					}
				}
				for _, s := range o.SubOptions {
					for _, p := range s.Include {
						if _, statErr := os.Stat(filepath.Join(path, p)); statErr != nil {
							problems = append(problems, Problem{path, "invalid_path", p})
						}
					}
				}
			}
			mods = append(mods, newMod(path, m, false))
			return filepath.SkipDir
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".zip" || ext == ".7z" || ext == ".rar" {
			if m, archiveErr := manifestFromArchive(path); archiveErr == nil {
				mods = append(mods, newMod(path, m, true))
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	orderRank := make(map[string]int, len(order))
	for n, id := range order {
		orderRank[strings.ToLower(id)] = n
	}
	sort.Slice(mods, func(i, j int) bool {
		pi, pj := settings.Pinned[mods[i].Manifest.GUID], settings.Pinned[mods[j].Manifest.GUID]
		if pi != pj {
			return !pi
		}
		ri, riOK := orderRank[strings.ToLower(mods[i].Manifest.GUID)]
		rj, rjOK := orderRank[strings.ToLower(mods[j].Manifest.GUID)]
		if !riOK {
			ri = len(order) + 1
		}
		if !rjOK {
			rj = len(order) + 1
		}
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(mods[i].Manifest.Name) < strings.ToLower(mods[j].Manifest.Name)
	})
	return mods, problems, nil
}

func SaveOrder(settings Settings, mods []*Mod) error {
	ids := make([]string, 0, len(mods))
	for _, mod := range mods {
		ids = append(ids, mod.Manifest.GUID)
	}
	b, _ := json.MarshalIndent(ids, "", "  ")
	if err := os.MkdirAll(configDir(settings.StorageDir), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir(settings.StorageDir), "order.json"), b, 0644)
}

func newMod(path string, manifest *Manifest, archive bool) *Mod {
	en := make([]bool, len(manifest.Options))
	for i := range en {
		en[i] = true
	}
	mod := &Mod{Directory: path, Manifest: *manifest, Enabled: true, EnabledOptions: en, SelectedOptions: make([]int, len(manifest.Options))}
	if archive {
		mod.ArchivePath = path
		mod.Directory = filepath.Dir(path)
	}
	return mod
}
func manifestFromArchive(path string) (*Manifest, error) {
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		if m, err := manifestFromNonZip(path); err == nil && m != nil {
			return m, nil
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return &Manifest{Version: -1, GUID: stableGUID(name), Name: name, Description: "A locally imported mod."}, nil
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.EqualFold(filepath.Base(f.Name), "manifest.json") {
			in, e := f.Open()
			if e != nil {
				return nil, e
			}
			data, e := io.ReadAll(in)
			in.Close()
			if e != nil {
				return nil, e
			}
			return parseManifest(data)
		}
	}
	// Some older mods do not ship a manifest. Match the upstream manager's
	// import behavior by inferring a legacy manifest from the archive name.
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if name == "" {
		return nil, errors.New("manifest.json not found in archive")
	}
	return &Manifest{
		Version:     -1,
		GUID:        stableGUID(name),
		Name:        name,
		Description: "A locally imported mod.",
	}, nil
}

func manifestFromNonZip(path string) (*Manifest, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".7z":
		r, err := sevenzip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		for _, f := range r.File {
			if !f.FileInfo().IsDir() && strings.EqualFold(filepath.Base(f.Name), "manifest.json") {
				in, err := f.Open()
				if err != nil {
					return nil, err
				}
				data, readErr := io.ReadAll(in)
				in.Close()
				if readErr != nil {
					return nil, readErr
				}
				return parseManifest(data)
			}
		}
	case ".rar":
		r, err := rardecode.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		for {
			h, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if !h.IsDir && strings.EqualFold(filepath.Base(h.Name), "manifest.json") {
				data, readErr := io.ReadAll(r)
				if readErr != nil {
					return nil, readErr
				}
				return parseManifest(data)
			}
			if !h.IsDir {
				_, _ = io.Copy(io.Discard, r)
			}
		}
	}
	return nil, errors.New("manifest.json not found")
}

func SavePinned(settings Settings, mods []*Mod) error {
	ids := make([]string, 0)
	for _, mod := range mods {
		if settings.Pinned[mod.Manifest.GUID] {
			ids = append(ids, mod.Manifest.GUID)
		}
	}
	b, _ := json.MarshalIndent(ids, "", "  ")
	if err := os.MkdirAll(configDir(settings.StorageDir), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir(settings.StorageDir), "pinned.json"), b, 0644)
}

func LoadPinned(settings *Settings) error {
	if settings.Pinned == nil {
		settings.Pinned = map[string]bool{}
	}
	b, err := readConfig(settings.StorageDir, "pinned.json")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var ids []string
	if err := json.Unmarshal(b, &ids); err != nil {
		return err
	}
	for _, id := range ids {
		settings.Pinned[id] = true
	}
	return nil
}

func parseManifest(data []byte) (*Manifest, error) {
	// Windows tools sometimes write UTF-8 JSON with a BOM. The JSON standard
	// parser rejects it, but it is safe to strip before decoding.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	// Normalize keys before dispatching between object and string options.
	normalized := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		normalized[strings.ToLower(key)] = value
	}
	raw = normalized
	var v int
	if x, ok := raw["version"]; ok {
		if err := json.Unmarshal(x, &v); err != nil {
			return nil, err
		}
	}
	if v != 0 && v != 1 {
		return nil, fmt.Errorf("unsupported manifest version %d", v)
	}
	options := raw["options"]
	delete(raw, "options")
	metadata, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(metadata, &m); err != nil {
		return nil, err
	}
	m.Version = v
	if m.Version == 0 {
		m.Version = -1
	}
	// Legacy manifests encode Options as an array of directory strings.
	if len(options) > 0 {
		if v == 1 {
			if err := json.Unmarshal(options, &m.Options); err != nil {
				return nil, err
			}
			return &m, nil
		}
		var legacy []string
		if err := json.Unmarshal(options, &legacy); err != nil {
			return nil, err
		} else {
			m.Options = make([]Option, len(legacy))
			for i, dir := range legacy {
				m.Options[i] = Option{Name: dir, Include: []string{dir}}
			}
		}
	}
	return &m, nil
}
func stableGUID(name string) string { return fmt.Sprintf("legacy-%x", fnv(name)) }
func fnv(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func LoadProfile(settings Settings, mods []*Mod) error {
	b, err := readConfig(settings.StorageDir, "enabled.json")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var es []ProfileEntry
	if err = json.Unmarshal(b, &es); err != nil {
		return err
	}
	by := map[string]*Mod{}
	for _, m := range mods {
		by[m.Manifest.GUID] = m
	}
	for _, e := range es {
		if m := by[e.GUID]; m != nil {
			m.Enabled = e.Enabled
			if len(e.Toggled) == len(m.EnabledOptions) {
				m.EnabledOptions = e.Toggled
			}
			if len(e.Selected) == len(m.SelectedOptions) {
				m.SelectedOptions = e.Selected
			}
		}
	}
	return nil
}
func SaveProfile(settings Settings, mods []*Mod) error {
	if err := os.MkdirAll(settings.StorageDir, 0755); err != nil {
		return err
	}
	es := make([]ProfileEntry, 0, len(mods))
	for _, m := range mods {
		es = append(es, ProfileEntry{m.Manifest.GUID, m.Enabled, m.EnabledOptions, m.SelectedOptions})
	}
	b, _ := json.MarshalIndent(es, "", "  ")
	if err := os.MkdirAll(configDir(settings.StorageDir), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir(settings.StorageDir), "enabled.json"), b, 0644)
}

// RemoveMod permanently removes an installed mod directory. Callers should
// save the profile again after reloading the remaining mods.
func RemoveMod(mod *Mod) error {
	if mod == nil {
		return errors.New("invalid mod")
	}
	if mod.ArchivePath != "" {
		return os.Remove(mod.ArchivePath)
	}
	if mod.Directory == "" {
		return errors.New("invalid mod")
	}
	return os.RemoveAll(mod.Directory)
}

func Purge(settings Settings) error {
	d := filepath.Join(settings.GameDir, "data")
	es, err := os.ReadDir(d)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range es {
		if !e.IsDir() && patchRE.MatchString(e.Name()) {
			if err := os.Remove(filepath.Join(d, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
func Deploy(settings Settings, mods []*Mod) error {
	return DeployWithProgress(settings, mods, nil)
}

func DeployWithProgress(settings Settings, mods []*Mod, progress func(done, total int)) error {
	defer func() { _ = CleanupTemp(settings) }()
	if err := Purge(settings); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(settings.GameDir, "data"), 0755); err != nil {
		return err
	}
	type patchSet struct {
		index int
		files map[string]string
	}
	groups := map[string][]patchSet{}
	total := 0
	for _, m := range mods {
		if m.Enabled {
			total++
		}
	}
	done := 0
	for _, m := range mods {
		if !m.Enabled {
			continue
		}
		working := m
		if m.ArchivePath != "" {
			dir, err := extractArchive(settings, m.ArchivePath)
			if err != nil {
				return err
			}
			// ZIP packages may contain a single top-level folder. Resolve the
			// manifest directory so option paths and patch files are relative to
			// the same root used by the package author.
			manifestDir, err := findManifestDir(dir)
			if err != nil {
				return fmt.Errorf("deployed archive has no manifest.json: %w", err)
			}
			clone := *m
			clone.Directory = manifestDir
			working = &clone
		}
		dirs := includeDirs(working)
		local := map[string]map[int]map[string]string{}
		for _, d := range dirs {
			es, _ := os.ReadDir(d)
			for _, e := range es {
				if !e.IsDir() && patchRE.MatchString(e.Name()) {
					x := patchRE.FindStringSubmatch(e.Name())
					if local[x[1]] == nil {
						local[x[1]] = map[int]map[string]string{}
					}
					var idx int
					_, _ = fmt.Sscanf(x[2], "%d", &idx)
					if local[x[1]][idx] == nil {
						local[x[1]][idx] = map[string]string{}
					}
					local[x[1]][idx][x[3]] = filepath.Join(d, e.Name())
				}
			}
		}
		for name, indexed := range local {
			indices := make([]int, 0, len(indexed))
			for idx := range indexed {
				indices = append(indices, idx)
			}
			sort.Ints(indices)
			for _, idx := range indices {
				groups[name] = append(groups[name], patchSet{index: idx, files: indexed[idx]})
			}
		}
		done++
		if progress != nil {
			progress(done, total)
		}
	}
	for name, sets := range groups {
		sort.SliceStable(sets, func(i, j int) bool { return sets[i].index < sets[j].index })
		offset := 0
		if settings.SkipList[name] {
			offset = 1
		}
		for i, set := range sets {
			for suffix, src := range set.files {
				dst := filepath.Join(settings.GameDir, "data", fmt.Sprintf("%s.patch_%d%s", name, i+offset, suffix))
				if err := copyFile(src, dst); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func extractArchive(settings Settings, archive string) (string, error) {
	dir := filepath.Join(settings.TempDir, "archive-"+fmt.Sprintf("%x", fnv(archive)))
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(archive), ".zip") {
		if err := extractArchiveContents(archive, dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer r.Close()
	for _, f := range r.File {
		p := filepath.Join(dir, filepath.Clean(f.Name))
		if !strings.HasPrefix(p, filepath.Clean(dir)+string(os.PathSeparator)) {
			return "", errors.New("archive contains unsafe path")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return "", err
		}
		in, e := f.Open()
		if e != nil {
			return "", e
		}
		out, e := os.Create(p)
		if e == nil {
			_, e = io.Copy(out, in)
			out.Close()
		}
		in.Close()
		if e != nil {
			return "", e
		}
		if modTime := f.ModTime(); !modTime.IsZero() {
			if e = os.Chtimes(p, modTime, modTime); e != nil {
				return "", e
			}
		}
	}
	return dir, nil
}

func extractArchiveContents(path, dest string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".7z":
		r, err := sevenzip.OpenReader(path)
		if err != nil {
			return err
		}
		defer r.Close()
		for _, f := range r.File {
			if err := extractArchiveEntry(dest, f.Name, f.FileInfo().IsDir(), f.FileInfo().ModTime(), f.Open); err != nil {
				return err
			}
		}
		return nil
	case ".rar":
		r, err := rardecode.OpenReader(path)
		if err != nil {
			return err
		}
		defer r.Close()
		for {
			h, err := r.Next()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := extractArchiveEntry(dest, h.Name, h.IsDir, h.ModificationTime, func() (io.ReadCloser, error) { return io.NopCloser(r), nil }); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Ext(path))
	}
}

func extractArchiveEntry(dest, name string, isDir bool, modTime time.Time, open func() (io.ReadCloser, error)) error {
	p := filepath.Join(dest, filepath.Clean(filepath.FromSlash(name)))
	if !strings.HasPrefix(p, filepath.Clean(dest)+string(os.PathSeparator)) {
		return errors.New("archive contains unsafe path")
	}
	if isDir {
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
		if !modTime.IsZero() {
			return os.Chtimes(p, modTime, modTime)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	in, err := open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(p)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !modTime.IsZero() {
		return os.Chtimes(p, modTime, modTime)
	}
	return nil
}

func findManifestDir(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(entry.Name(), "manifest.json") {
			found = filepath.Dir(path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		// Manifest-less archives are inferred during import. If the archive
		// contains a single wrapper directory, use it as the package root.
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return "", readErr
		}
		var dirs []string
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, filepath.Join(root, entry.Name()))
			} else if patchRE.MatchString(entry.Name()) {
				return root, nil
			}
		}
		if len(dirs) == 1 {
			return dirs[0], nil
		}
		if len(entries) > 0 {
			return root, nil
		}
		return "", errors.New("manifest.json not found")
	}
	return found, nil
}

func includeDirs(m *Mod) []string {
	var out []string
	if len(m.Manifest.Options) == 0 {
		return []string{m.Directory}
	}
	// Legacy manifests describe mutually exclusive folders. Only the selected
	// folder is deployed, matching the original manager's edit behavior.
	if m.Manifest.Version == -1 {
		choice := 0
		if len(m.SelectedOptions) > 0 && m.SelectedOptions[0] >= 0 && m.SelectedOptions[0] < len(m.Manifest.Options) {
			choice = m.SelectedOptions[0]
		}
		return paths(m.Directory, m.Manifest.Options[choice].Include)
	}
	for i, o := range m.Manifest.Options {
		if i >= len(m.EnabledOptions) || !m.EnabledOptions[i] {
			continue
		}
		out = append(out, paths(m.Directory, o.Include)...)
		if len(o.SubOptions) > 0 && i < len(m.SelectedOptions) {
			j := m.SelectedOptions[i]
			if j >= 0 && j < len(o.SubOptions) {
				out = append(out, paths(m.Directory, o.SubOptions[j].Include)...)
			}
		}
	}
	return out
}
func paths(root string, ps []string) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, filepath.Join(root, p))
	}
	return out
}
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
func ImportArchive(settings Settings, archive string) (*Mod, error) {
	return importArchive(settings, archive, false)
}

func ImportArchiveReplacing(settings Settings, archive string) (*Mod, error) {
	return importArchive(settings, archive, true)
}

func importArchive(settings Settings, archive string, replace bool) (*Mod, error) {
	manifest, err := manifestFromArchive(archive)
	if err != nil {
		return nil, err
	}
	existing, _, err := Load(settings)
	if err != nil {
		return nil, err
	}
	for _, mod := range existing {
		if manifest.GUID != "" && strings.EqualFold(mod.Manifest.GUID, manifest.GUID) {
			if !replace {
				return nil, fmt.Errorf("duplicate mod GUID: %s", manifest.GUID)
			}
			if mod.ArchivePath != "" {
				if err := os.Remove(mod.ArchivePath); err != nil && !os.IsNotExist(err) {
					return nil, err
				}
			} else if err := os.RemoveAll(filepath.Join(settings.StorageDir, "Mods", filepath.Base(mod.Directory))); err != nil {
				return nil, err
			}
		}
	}
	workRoot, err := extractArchive(settings, archive)
	if err != nil {
		return nil, err
	}
	manifestDir, err := findManifestDir(workRoot)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(manifestDir, "manifest.json")); os.IsNotExist(err) {
		data, _ := json.MarshalIndent(manifest, "", "  ")
		manifestPath := filepath.Join(manifestDir, "manifest.json")
		// Capture the newest original file before writing the generated manifest;
		// otherwise the generated file would look like a fresh modification.
		originalTime := latestFileModTime(manifestDir)
		if originalTime.IsZero() {
			if info, statErr := os.Stat(archive); statErr == nil {
				originalTime = info.ModTime()
			}
		}
		if err := os.WriteFile(manifestPath, data, 0644); err != nil {
			return nil, err
		}
		// The generated manifest must not make the imported mod appear newly
		// modified. Keep its timestamp aligned with the newest original file.
		if !originalTime.IsZero() {
			if err := os.Chtimes(manifestPath, originalTime, originalTime); err != nil {
				return nil, err
			}
		}
	}
	name := manifest.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(archive), filepath.Ext(archive))
	}
	name = strings.NewReplacer("\\", "_", "/", "_", ":", "_").Replace(name)
	dest := filepath.Join(settings.StorageDir, "Mods", name)
	if _, err := os.Stat(dest); err == nil {
		if !replace {
			return nil, errors.New("mod already exists")
		}
		if err := os.Remove(dest); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return nil, err
	}
	if err := copyDir(manifestDir, dest); err != nil {
		return nil, err
	}
	mods, _, err := Load(settings)
	if err != nil {
		return nil, err
	}
	for _, m := range mods {
		if filepath.Clean(m.Directory) == filepath.Clean(dest) {
			return m, nil
		}
	}
	return nil, errors.New("imported manifest not found")
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func latestFileModTime(root string) time.Time {
	var latest time.Time
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest
}
