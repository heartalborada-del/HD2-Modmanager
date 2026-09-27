package manager

import (
	"os"
	"path/filepath"
	"strings"
)

// CheckIssue uses stable message keys so the GUI can localize findings.
type CheckIssue struct {
	Mod, Kind, Detail string
}

// CheckMods inspects the currently selected option files without deploying or
// modifying anything. Shared patch targets are warnings, not proof of a broken mod.
func CheckMods(mods []*Mod, progress func(int, int)) []CheckIssue {
	var issues []CheckIssue
	owners := map[string]string{}
	for i, mod := range mods {
		add := func(kind, detail string) {
			issues = append(issues, CheckIssue{mod.Manifest.Name, kind, detail})
		}
		if mod.ArchivePath != "" {
			add("Archive check unavailable", mod.ArchivePath)
		} else {
			count := 0
			seen := map[string]bool{}
			for _, dir := range includeDirs(mod) {
				entries, err := os.ReadDir(dir)
				if err != nil {
					add("Cannot read option folder", dir+": "+err.Error())
					continue
				}
				for _, entry := range entries {
					match := patchRE.FindStringSubmatch(entry.Name())
					if entry.IsDir() || match == nil {
						continue
					}
					path := filepath.Join(dir, entry.Name())
					if seen[path] {
						continue
					}
					seen[path] = true
					info, err := entry.Info()
					if err != nil {
						add("Cannot read patch", path)
						continue
					}
					if info.Size() == 0 {
						add("Empty patch file", path)
					}
					if match[3] != "" {
						if _, err := os.Stat(strings.TrimSuffix(path, match[3])); err != nil {
							add("Missing base patch", path)
						}
						continue
					}
					count++
					if mod.Enabled {
						if owner, ok := owners[match[1]]; ok && owner != mod.Directory {
							add("Shared patch target", match[1])
						}
						owners[match[1]] = mod.Directory
					}
				}
			}
			if count == 0 {
				add("No selected patch files", mod.Directory)
			}
		}
		if progress != nil {
			progress(i+1, len(mods))
		}
	}
	return issues
}
