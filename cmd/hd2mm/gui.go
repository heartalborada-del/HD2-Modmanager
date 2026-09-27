//go:build gui

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AllenDang/cimgui-go/backend"
	"github.com/AllenDang/cimgui-go/backend/glfwbackend"
	"github.com/AllenDang/cimgui-go/imgui"
	_ "github.com/AllenDang/cimgui-go/impl/glfw"
	"hd2modmanager/internal/manager"
)

type uiConfig struct {
	FontScale float32 `json:"fontScale"`
	IconScale float32 `json:"iconScale"`
	Language  string  `json:"language"`
	ShowIcons bool    `json:"showIcons"`
}

func updateIMEPosition() {
	if !imgui.IsItemActive() {
		return
	}
	min := imgui.ItemRectMin()
	max := imgui.ItemRectMax()
	if viewport := imgui.MainViewport(); viewport != nil {
		viewportPos := viewport.Pos()
		setIMEPosition(viewport.PlatformHandleRaw(), int(min.X-viewportPos.X), int(max.Y-viewportPos.Y), int(max.Y-min.Y))
	}
}

func detailOptionSeparator() {
	// Keep the boundary visible with the dark theme used by the detail dialog.
	imgui.PushStyleColorVec4(imgui.ColSeparator, imgui.NewVec4(0.32, 0.38, 0.46, 1))
	imgui.Separator()
	imgui.PopStyleColor()
}

func modDiskInfo(root string) (int64, time.Time) {
	var size int64
	var latest time.Time
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		size += info.Size()
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return size, latest
}

func formatModSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", size)
}

func uiConfigPath(storage string) string {
	return filepath.Join(storage, "config", "ui.json")
}

func readUIConfig(storage string) ([]byte, error) {
	path := uiConfigPath(storage)
	legacy := filepath.Join(storage, "ui.json")
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
		if useLegacy && os.MkdirAll(filepath.Dir(path), 0755) == nil {
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

func loadUIConfig(storage string) float32 {
	b, err := readUIConfig(storage)
	if err != nil {
		return 1.15
	}
	var c uiConfig
	if json.Unmarshal(b, &c) != nil || c.FontScale < 1.0 || c.FontScale > 3.0 {
		return 1.15
	}
	return c.FontScale
}
func loadLanguage(storage string) string {
	b, err := readUIConfig(storage)
	if err != nil {
		return "en"
	}
	var c uiConfig
	if json.Unmarshal(b, &c) != nil || (c.Language != "en" && c.Language != "zh-CN") {
		return "en"
	}
	return c.Language
}
func loadIconScale(storage string) float32 {
	b, err := readUIConfig(storage)
	if err != nil {
		return 1
	}
	var c uiConfig
	if json.Unmarshal(b, &c) != nil || c.IconScale < 0.5 || c.IconScale > 2 {
		return 1
	}
	return c.IconScale
}
func loadShowIcons(storage string) bool {
	b, err := readUIConfig(storage)
	if err != nil {
		return true
	}
	var c struct {
		ShowIcons *bool `json:"showIcons"`
	}
	if json.Unmarshal(b, &c) != nil || c.ShowIcons == nil {
		return true
	}
	return *c.ShowIcons
}
func saveUIConfig(storage string, scale, iconScale float32, language string, showIcons bool) {
	path := uiConfigPath(storage)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	b, _ := json.MarshalIndent(uiConfig{FontScale: scale, IconScale: iconScale, Language: language, ShowIcons: showIcons}, "", "  ")
	_ = os.WriteFile(path, b, 0644)
}

var translations = map[string][2]string{
	"Mods": {"Mods", "模组"}, "Install": {"Install / Deploy", "导入与部署"}, "Configuration": {"Configuration", "配置"}, "Deploy": {"Deploy", "部署"},
	"Installed mods": {"Installed mods", "已安装模组"}, "Search mods...": {"Search mods...", "搜索模组..."}, "Clear search": {"Clear search", "清除搜索"}, "Install ZIP mods into": {"Install ZIP mods into", "Mod存储目录"}, "Windows picker help": {"The Windows picker supports selecting multiple ZIP files. They are imported into the Mods folder next to the executable by default.", "Windows 文件选择器支持多选文件"}, "mods selected": {"Mod(s) selected", "个模组已选择"}, "archives selected": {"selected", "个文件已选择"}, "Choose at least one ZIP file": {"Choose at least one ZIP file", "请至少选择一个压缩文件"}, "Installed": {"Installed", "已导入"}, "Install failed": {"Install failed", "导入失败"},
	"Select all": {"Select all", "全选"}, "Clear selection": {"Clear selection", "清除选择"}, "Enable selected": {"Enable selected", "启用选中"}, "Disable selected": {"Disable selected", "禁用选中"}, "Remove selected": {"Remove selected", "移除选中"}, "Details": {"Details", "详情"}, "Mod details": {"Mod details", "模组详情"}, "Save option changes": {"Save option changes", "保存选项更改"}, "Choose ZIP files...": {"Choose ZIP files...", "选择压缩文件..."}, "Clear selected files": {"Clear selected files", "清除已选文件"}, "Remove file": {"Remove file", "移除文件"}, "Category delete notice": {"Mods in this category will be moved to the Mods root folder.", "此分类中的 Mod 将移动到 Mod 根目录"}, "OK": {"OK", "确定"}, "Install selected mods": {"Install selected mods", "导入选中模组"},
	"Pin": {"Pin", "固定"}, "Unpin": {"Unpin", "取消固定"}, "Delete": {"Delete", "删除"}, "New category": {"New category", "新建分类"}, "Delete category": {"Delete category", "删除分类"}, "Create category": {"Create category", "创建分类"}, "Category name": {"Category name", "分类名称"}, "Category created": {"Category created", "分类已创建"}, "Move to category": {"Move to category", "移动到分类"}, "Select category": {"Select category", "选择分类"}, "Move": {"Move", "移动"}, "Create a category first": {"Create a category first", "请先创建分类"}, "Categories": {"Categories", "分类"}, "All": {"All", "全部"},
	"Paths": {"Paths", "路径"}, "Game directory": {"Game directory", "游戏目录"}, "Storage directory": {"Storage directory", "数据目录"}, "Appearance": {"Appearance", "外观"}, "Show mod icons": {"Show mod icons", "显示 Mod 图标"}, "Icon scale": {"Icon scale", "图标缩放"}, "Font scale": {"Font scale", "字体缩放"}, "Reset font scale": {"Reset font scale", "重置字体缩放"}, "Apply paths": {"Apply paths", "应用路径"}, "Language": {"Language", "语言"}, "Launch HD2": {"Launch HD2", "启动 HD2"}, "Save profile": {"Save profile", "保存配置"}, "Deploy enabled mods": {"Deploy enabled mods", "部署已启用模组"}, "Purge deployed files": {"Purge deployed files", "清除已部署文件"}, "Replace existing mod?": {"Replace existing mod?", "替换现有模组？"}, "Replace": {"Replace", "替换"}, "Cancel": {"Cancel", "取消"}, "Conflict with": {"Conflict with", "冲突模组"}, "Replace this mod?": {"Replace this mod?", "是否替换此模组？"}, "Import error": {"Import error", "导入错误"}, "Processing...": {"Processing...", "处理中..."},
}

func tr(language, key string) string {
	if pair, ok := translations[key]; ok && language == "zh-CN" {
		return pair[1]
	}
	return key
}

func ellipsizeText(text string, maxWidth float32) string {
	if maxWidth <= 0 {
		return ""
	}
	if imgui.CalcTextSizeV(text, false, 0).X <= maxWidth {
		return text
	}
	const suffix = "..."
	runes := []rune(text)
	for n := len(runes) - 1; n >= 0; n-- {
		candidate := string(runes[:n]) + suffix
		if imgui.CalcTextSizeV(candidate, false, 0).X <= maxWidth {
			return candidate
		}
	}
	return suffix
}

func categoryFolders(storage string) []string {
	entries, err := os.ReadDir(filepath.Join(storage, "Mods"))
	if err != nil {
		return nil
	}
	var categories []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		categoryPath := filepath.Join(storage, "Mods", entry.Name())
		if _, err := os.Stat(filepath.Join(categoryPath, ".category")); err == nil {
			categories = append(categories, entry.Name())
			continue
		}
		// Keep folders created by older versions visible and upgrade them to
		// marked categories when the user moves a mod into them.
		if _, err := os.Stat(filepath.Join(categoryPath, "manifest.json")); os.IsNotExist(err) {
			categories = append(categories, entry.Name())
		}
	}
	sort.Strings(categories)
	return categories
}

type importBatchResult struct {
	installed     int
	processed     int
	duplicate     string
	duplicateErr  string
	duplicates    []string
	duplicateErrs []string
	err           error
}

type deployProgress struct {
	done, total int
	err         error
	finished    bool
}

func replaceAsync(settings manager.Settings, archive string) <-chan error {
	out := make(chan error, 1)
	go func() {
		_, err := manager.ImportArchiveReplacing(settings, archive)
		_ = manager.CleanupTemp(settings)
		out <- err
		close(out)
	}()
	return out
}

func importArchivesParallel(settings manager.Settings, archives []string) <-chan importBatchResult {
	out := make(chan importBatchResult, 1)
	go func() {
		defer close(out)
		workers := runtime.NumCPU()
		if workers < 2 {
			workers = 2
		}
		if workers > len(archives) {
			workers = len(archives)
		}
		jobs := make(chan string)
		var wg sync.WaitGroup
		var mu sync.Mutex
		result := importBatchResult{}
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for archive := range jobs {
					_, err := manager.ImportArchive(settings, archive)
					mu.Lock()
					result.processed++
					if err == nil {
						result.installed++
					} else if strings.Contains(err.Error(), "duplicate mod GUID") {
						result.duplicates = append(result.duplicates, archive)
						result.duplicateErrs = append(result.duplicateErrs, strings.TrimSpace(strings.TrimPrefix(err.Error(), "duplicate mod GUID:")))
						if result.duplicate == "" {
							result.duplicate = archive
							result.duplicateErr = err.Error()
						}
					} else if result.err == nil {
						result.err = err
					}
					mu.Unlock()
				}
			}()
		}
		for _, archive := range archives {
			jobs <- archive
		}
		close(jobs)
		wg.Wait()
		_ = manager.CleanupTemp(settings)
		out <- result
	}()
	return out
}

func deployAsync(settings manager.Settings, mods []*manager.Mod) <-chan deployProgress {
	out := make(chan deployProgress, 8)
	go func() {
		defer close(out)
		err := manager.DeployWithProgress(settings, mods, func(done, total int) {
			out <- deployProgress{done: done, total: total}
		})
		out <- deployProgress{err: err, finished: true}
	}()
	return out
}
func loadModIcons(mods []*manager.Mod) []*backend.Texture {
	icons := make([]*backend.Texture, len(mods))
	for i, mod := range mods {
		if mod.Manifest.IconPath == "" {
			continue
		}
		if mod.ArchivePath != "" {
			if r, err := zip.OpenReader(mod.ArchivePath); err == nil {
				for _, f := range r.File {
					if filepath.ToSlash(f.Name) == filepath.ToSlash(mod.Manifest.IconPath) || filepath.Base(f.Name) == filepath.Base(mod.Manifest.IconPath) {
						if in, e := f.Open(); e == nil {
							if data, e := io.ReadAll(in); e == nil {
								if decoded, _, e := image.Decode(bytes.NewReader(data)); e == nil {
									icons[i] = backend.NewTextureFromRgba(backend.ImageToRgba(decoded))
								}
							}
							in.Close()
						}
						break
					}
				}
				r.Close()
			}
		} else if rgba, err := backend.LoadImage(filepath.Join(mod.Directory, mod.Manifest.IconPath)); err == nil {
			icons[i] = backend.NewTextureFromRgba(rgba)
		}
	}
	return icons
}
func newPlaceholderTexture() *backend.Texture {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 38, G: 55, B: 78, A: 255}}, image.Point{}, draw.Src)
	for x := 0; x < 64; x++ {
		img.SetRGBA(x, x, color.RGBA{R: 100, G: 130, B: 170, A: 255})
		img.SetRGBA(63-x, x, color.RGBA{R: 100, G: 130, B: 170, A: 255})
	}
	return backend.NewTextureFromRgba(img)
}
func releaseModIcons(icons []*backend.Texture) {
	for _, icon := range icons {
		if icon != nil {
			icon.Release()
		}
	}
}

func cjkFontPath() string {
	candidates := []string{
		filepath.Join(os.Getenv("WINDIR"), "Fonts", "msyh.ttc"),
		filepath.Join(os.Getenv("WINDIR"), "Fonts", "NotoSansCJK-Regular.ttc"),
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"/System/Library/Fonts/PingFang.ttc",
	}
	for _, path := range candidates {
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

func addCJKFont() {
	path := cjkFontPath()
	if path == "" {
		return
	}
	// Merge the system CJK font into the default UI font. Building explicit
	// ranges keeps the atlas smaller than loading every glyph in the font.
	builder := imgui.NewFontGlyphRangesBuilder()
	for _, r := range [][2]uint32{{0x2E80, 0x2FFF}, {0x3040, 0x30FF}, {0x3400, 0x4DBF}, {0x4E00, 0x9FFF}, {0xAC00, 0xD7AF}} {
		for ch := r[0]; ch <= r[1]; ch++ {
			builder.AddChar(imgui.Wchar(ch))
		}
	}
	ranges := imgui.NewGlyphRange()
	builder.BuildRanges(ranges)
	cfg := imgui.NewFontConfig()
	cfg.SetMergeMode(true)
	// Use an implicit reference size so it can be merged into the default
	// vector font without triggering ImGui's reference-size assertion.
	imgui.CurrentIO().Fonts().AddFontFromFileTTFV(path, 0, cfg, ranges.Data())
}

func init() {
	runtime.LockOSThread()
	translations["Modified"] = [2]string{"Modified", "\u4fee\u6539\u65e5\u671f"}
	translations["Size"] = [2]string{"Size", "\u5927\u5c0f"}
	translations["Deployment purged"] = [2]string{"Deployment purged", "\u5df2\u6e05\u9664\u5df2\u90e8\u7f72\u6587\u4ef6"}
	translations["Loaded status"] = [2]string{"Loaded %d mods, %d warnings", "\u5df2\u52a0\u8f7d %d \u4e2a\u6a21\u7ec4\uff0c%d \u4e2a\u8b66\u544a"}
	translations["Pin to bottom"] = [2]string{"Pin to bottom", "\u7f6e\u5e95"}
	translations["Remove from bottom"] = [2]string{"Remove from bottom", "\u53d6\u6d88\u7f6e\u5e95"}
}

// runGUI owns one top-level window. Pages are tabs and child regions inside
// it, so installing a mod never opens a second application window.
func runGUI(settings manager.Settings) error {
	if err := manager.LoadPinned(&settings); err != nil {
		return err
	}
	mods, problems, err := manager.Load(settings)
	if err != nil {
		return err
	}
	if err := manager.LoadProfile(settings, mods); err != nil {
		return err
	}
	b, err := backend.CreateBackend(glfwbackend.NewGLFWBackend())
	if err != nil {
		return err
	}
	b.SetBgColor(imgui.NewVec4(0.055, 0.07, 0.095, 1))
	b.SetAfterCreateContextHook(func() {
		io := imgui.CurrentIO()
		io.Fonts().AddFontDefaultVector()
		addCJKFont()
		imgui.CurrentStyle().SetWindowBorderHoverPadding(1)
	})
	b.CreateWindow("HD2 Mod Manager", 1200, 760)
	// The GLFW backend enables docking after the context hook runs. Clear it
	// after window creation so no DockSpace drop target or docking preview can
	// be created while dragging any ImGui window.
	io := imgui.CurrentIO()
	io.SetConfigFlags(io.ConfigFlags() &^ imgui.ConfigFlagsDockingEnable)
	io.SetConfigDockingNoDockingOver(true)
	io.SetConfigDockingNoSplit(true)

	fontScale := loadUIConfig(settings.StorageDir)
	iconScale := loadIconScale(settings.StorageDir)
	language := loadLanguage(settings.StorageDir)
	showIcons := loadShowIcons(settings.StorageDir)
	placeholderIcon := newPlaceholderTexture()
	archivePaths := []string{}
	icons := make([]*backend.Texture, len(mods))
	iconsLoaded := false
	status := fmt.Sprintf(tr(language, "Loaded status"), len(mods), len(problems))
	selected := map[int]bool{}
	detailIndex := -1
	detailInfoIndex := -2
	detailSizeBytes := int64(0)
	detailModified := time.Time{}
	detailsVisible := false
	detailFocusPending := false
	detailSizePending := false
	replaceVisible := false
	replaceArchive := ""
	replaceName := ""
	errorVisible := false
	errorMessage := ""
	categoryVisible := false
	categoryName := ""
	deleteCategoryVisible := false
	deleteCategoryName := ""
	moveVisible := false
	moveCategory := ""
	activeCategory := ""
	orderChanged := false
	installBusy := false
	installResultCh := (<-chan importBatchResult)(nil)
	deployBusy := false
	deployProgressCh := (<-chan deployProgress)(nil)
	deployDone, deployTotal := 0, 0
	replaceBusy := false
	replaceResultCh := (<-chan error)(nil)
	modSearch := ""
	lastScale := fontScale
	appliedStyleScale := float32(1)
	checkUI := modCheckUI{}
	b.Run(func() {
		if installBusy && installResultCh != nil {
			select {
			case result := <-installResultCh:
				installBusy = false
				if result.err != nil {
					errorMessage = result.err.Error()
					errorVisible = true
				}
				if len(result.duplicates) > 0 || result.duplicate != "" {
					replaceArchive = result.duplicate
					if replaceArchive == "" {
						replaceArchive = result.duplicates[0]
					}
					lookup := mods
					if existing, _, loadErr := manager.Load(settings); loadErr == nil {
						lookup = existing
					}
					names := []string{}
					seenNames := map[string]bool{}
					for _, guid := range result.duplicateErrs {
						name := guid
						for _, mod := range lookup {
							if strings.EqualFold(mod.Manifest.GUID, guid) && mod.Manifest.Name != "" {
								name = mod.Manifest.Name
								break
							}
						}
						if !seenNames[name] {
							names = append(names, name)
							seenNames[name] = true
						}
					}
					if len(names) == 0 {
						names = append(names, filepath.Base(replaceArchive))
					}
					replaceName = strings.Join(names, "\n")
					replaceVisible = true
				}
				if result.installed > 0 {
					releaseModIcons(icons)
					mods, problems, _ = manager.Load(settings)
					_ = manager.LoadProfile(settings, mods)
					icons = loadModIcons(mods)
					selected = map[int]bool{}
					archivePaths = nil
				}
			default:
			}
		}
		if deployBusy && deployProgressCh != nil {
			select {
			case progress, ok := <-deployProgressCh:
				if !ok || progress.finished {
					deployBusy = false
					if progress.err != nil {
						errorMessage = progress.err.Error()
						errorVisible = true
					}
				} else {
					deployDone, deployTotal = progress.done, progress.total
				}
			default:
			}
		}
		if replaceBusy && replaceResultCh != nil {
			select {
			case err, ok := <-replaceResultCh:
				if ok {
					replaceBusy = false
					replaceVisible = false
					if err != nil {
						errorMessage = err.Error()
						errorVisible = true
					} else {
						releaseModIcons(icons)
						mods, problems, _ = manager.Load(settings)
						_ = manager.LoadProfile(settings, mods)
						icons = loadModIcons(mods)
						archivePaths = nil
					}
				}
			default:
			}
		}
		// Load textures only after the backend has entered its render loop. On
		// the first frame the OpenGL context and texture manager are guaranteed
		// to be ready.
		if !iconsLoaded {
			icons = loadModIcons(mods)
			iconsLoaded = true
		}
		imgui.CurrentStyle().SetFontScaleMain(fontScale)
		if fontScale != appliedStyleScale {
			imgui.CurrentStyle().ScaleAllSizes(fontScale / appliedStyleScale)
			appliedStyleScale = fontScale
		}
		// ImGui validates this value while processing hover/drag events.
		// Keep it positive even when a backend or style reset restores zero.
		if imgui.CurrentStyle().WindowBorderHoverPadding() <= 0 {
			imgui.CurrentStyle().SetWindowBorderHoverPadding(1)
		}
		// Keep layout spacing stable while the font and icons scale.
		style := imgui.CurrentStyle()
		style.SetWindowPadding(imgui.NewVec2(8, 8))
		style.SetFramePadding(imgui.NewVec2(4, 3))
		style.SetItemSpacing(imgui.NewVec2(8, 4))
		style.SetItemInnerSpacing(imgui.NewVec2(4, 4))
		if fontScale != lastScale {
			saveUIConfig(settings.StorageDir, fontScale, iconScale, language, showIcons)
			lastScale = fontScale
		}
		viewport := imgui.MainViewport()
		imgui.SetNextWindowPosV(viewport.WorkPos(), imgui.CondAlways, imgui.NewVec2(0, 0))
		imgui.SetNextWindowSizeV(viewport.WorkSize(), imgui.CondAlways)
		flags := imgui.WindowFlagsNoTitleBar | imgui.WindowFlagsNoResize | imgui.WindowFlagsNoMove | imgui.WindowFlagsNoCollapse | imgui.WindowFlagsNoSavedSettings
		if !imgui.BeginV("##HD2MainWindow", nil, flags) {
			imgui.End()
			return
		}
		if imgui.BeginTabBar("main-pages") {
			if imgui.BeginTabItem(tr(language, "Mods")) {
				selectedCount := 0
				for i, isSelected := range selected {
					if isSelected && i < len(mods) {
						selectedCount++
					}
				}
				imgui.Text(fmt.Sprintf("%s (%d %s)", tr(language, "Installed mods"), selectedCount, tr(language, "mods selected")))
				categories := categoryFolders(settings.StorageDir)
				imgui.Spacing()
				if imgui.BeginTabBar("mod-categories") {
					if imgui.BeginTabItem(tr(language, "All")) {
						activeCategory = ""
						imgui.EndTabItem()
					}
					for _, categoryName := range categories {
						if imgui.BeginTabItem(categoryName) {
							activeCategory = categoryName
							imgui.EndTabItem()
						}
					}
					imgui.EndTabBar()
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "New category")) {
					categoryName = ""
					categoryVisible = true
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Move to category")) && selectedCount > 0 {
					moveVisible = true
					moveCategory = ""
				}
				if activeCategory != "" {
					imgui.SameLine()
					if imgui.Button(tr(language, "Delete category")) {
						deleteCategoryName = activeCategory
						deleteCategoryVisible = true
					}
				}
				imgui.InputTextWithHint("##mod-search", tr(language, "Search mods..."), &modSearch, imgui.InputTextFlagsNone, nil)
				// GLFW forwards committed characters, but its cimgui backend does not
				// position the native Windows IME composition window. Keep it anchored
				// to the active ImGui input item so the candidate list follows the field.
				updateIMEPosition()
				imgui.SameLine()
				if imgui.Button(tr(language, "Clear search")) {
					modSearch = ""
				}
				query := strings.ToLower(strings.TrimSpace(modSearch))
				visibleMods := map[int]string{}
				for i, mod := range mods {
					category := ""
					if rel, err := filepath.Rel(filepath.Join(settings.StorageDir, "Mods"), mod.Directory); err == nil {
						parts := strings.Split(rel, string(os.PathSeparator))
						if len(parts) > 1 {
							category = parts[0]
						}
					}
					if activeCategory != "" && category != activeCategory {
						continue
					}
					if query != "" && !strings.Contains(strings.ToLower(mod.Manifest.Name), query) && !strings.Contains(strings.ToLower(mod.Manifest.Description), query) && !strings.Contains(strings.ToLower(category), query) {
						continue
					}
					visibleMods[i] = category
				}
				if imgui.Button(tr(language, "Select all")) {
					selected = map[int]bool{}
					for i := range visibleMods {
						selected[i] = true
					}
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Clear selection")) {
					selected = map[int]bool{}
				}
				imgui.SameLine()
				imgui.InternalSeparatorExV(imgui.SeparatorFlagsVertical, 3)
				imgui.SameLine()
				if imgui.Button(tr(language, "Enable selected")) {
					for i := range mods {
						if selected[i] {
							mods[i].Enabled = true
						}
					}
					_ = manager.SaveProfile(settings, mods)
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Disable selected")) {
					for i := range mods {
						if selected[i] {
							mods[i].Enabled = false
						}
					}
					_ = manager.SaveProfile(settings, mods)
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Remove selected")) {
					removed := 0
					for i, mod := range mods {
						if selected[i] {
							if err := manager.RemoveMod(mod); err == nil {
								removed++
							}
						}
					}
					if removed > 0 {
						releaseModIcons(icons)
						mods, problems, _ = manager.Load(settings)
						icons = loadModIcons(mods)
						selected = map[int]bool{}
						_ = manager.SaveProfile(settings, mods)
						status = fmt.Sprintf("Removed %d mod(s)", removed)
					}
				}
				imgui.Separator()
				if imgui.BeginChildStr("mod-list") {
					iconSize := float32(64) * fontScale * iconScale
					for i, mod := range mods {
						category, visible := visibleMods[i]
						if !visible {
							continue
						}
						chosen := selected[i]
						if imgui.Checkbox(fmt.Sprintf("##select-%d", i), &chosen) {
							selected[i] = chosen
						}
						imgui.SameLine()
						rowY := imgui.CursorPos().Y
						if showIcons {
							icon := placeholderIcon
							if i < len(icons) && icons[i] != nil {
								icon = icons[i]
							}
							iconPos := imgui.CursorPos()
							iconScreenPos := imgui.CursorScreenPos()
							imgui.Image(icon.ID, imgui.NewVec2(iconSize, iconSize))
							if imgui.IsMouseHoveringRect(iconScreenPos, imgui.NewVec2(iconScreenPos.X+iconSize, iconScreenPos.Y+iconSize)) {
								imgui.SetCursorPos(iconPos)
								transparent := imgui.NewVec4(0, 0, 0, 0)
								imgui.PushStyleColorVec4(imgui.ColButton, transparent)
								imgui.PushStyleColorVec4(imgui.ColButtonHovered, transparent)
								imgui.PushStyleColorVec4(imgui.ColButtonActive, transparent)
								up := imgui.ButtonV(fmt.Sprintf("▲##move-up-%d", i), imgui.NewVec2(iconSize, iconSize/2))
								imgui.SetCursorPos(imgui.NewVec2(iconPos.X, iconPos.Y+iconSize/2))
								down := imgui.ButtonV(fmt.Sprintf("▼##move-down-%d", i), imgui.NewVec2(iconSize, iconSize/2))
								imgui.PopStyleColorV(3)
								imgui.SetCursorPos(imgui.NewVec2(iconPos.X+iconSize, iconPos.Y))
								if up && i > 0 && settings.Pinned[mods[i].Manifest.GUID] == settings.Pinned[mods[i-1].Manifest.GUID] {
									mods[i], mods[i-1] = mods[i-1], mods[i]
									icons[i], icons[i-1] = icons[i-1], icons[i]
									selected[i], selected[i-1] = selected[i-1], selected[i]
									_ = manager.SaveOrder(settings, mods)
									orderChanged = true
								}
								if down && i+1 < len(mods) && settings.Pinned[mods[i].Manifest.GUID] == settings.Pinned[mods[i+1].Manifest.GUID] {
									mods[i], mods[i+1] = mods[i+1], mods[i]
									icons[i], icons[i+1] = icons[i+1], icons[i]
									selected[i], selected[i+1] = selected[i+1], selected[i]
									_ = manager.SaveOrder(settings, mods)
									orderChanged = true
								}
							}
							imgui.SameLine()
						}
						if showIcons {
							imgui.SetCursorPosY(rowY)
						}
						displayName := mod.Manifest.Name
						if category != "" && activeCategory == "" {
							displayName = fmt.Sprintf("[%s] %s", category, displayName)
						}
						actionColumn := imgui.WindowWidth() - 400
						if actionColumn < 260 {
							actionColumn = 260
						}
						cursorX := imgui.CursorPosX()
						titleY := imgui.CursorPos().Y
						imgui.Text(ellipsizeText(displayName, actionColumn-cursorX-16))
						if imgui.CursorPosX() >= actionColumn {
							actionColumn = imgui.CursorPosX() + 8
						}
						imgui.SetCursorPos(imgui.NewVec2(actionColumn, titleY))
						v := mod.Enabled
						if imgui.Checkbox(fmt.Sprintf("##enabled-%d", i), &v) {
							mod.Enabled = v
							_ = manager.SaveProfile(settings, mods)
						}
						imgui.SameLine()
						if imgui.Button(fmt.Sprintf("%s##%d", tr(language, "Details"), i)) {
							selected[i] = true
							detailIndex = i
							detailsVisible = true
							detailFocusPending = true
							detailSizePending = true
						}
						imgui.SameLine()
						pinLabel := fmt.Sprintf("%s##%d", tr(language, "Pin to bottom"), i)
						if settings.Pinned[mod.Manifest.GUID] {
							pinLabel = fmt.Sprintf("%s##%d", tr(language, "Remove from bottom"), i)
						}
						if imgui.Button(pinLabel) {
							settings.Pinned[mod.Manifest.GUID] = !settings.Pinned[mod.Manifest.GUID]
							_ = manager.SavePinned(settings, mods)
							releaseModIcons(icons)
							mods, _, _ = manager.Load(settings)
							_ = manager.LoadProfile(settings, mods)
							icons = loadModIcons(mods)
							selected = map[int]bool{}
							status = "Pinned order updated"
						}
						imgui.SameLine()
						if imgui.Button(fmt.Sprintf("%s##%d", tr(language, "Delete"), i)) {
							if err := manager.RemoveMod(mod); err != nil {
								status = err.Error()
							} else {
								releaseModIcons(icons)
								mods, problems, _ = manager.Load(settings)
								_ = manager.LoadProfile(settings, mods)
								icons = loadModIcons(mods)
								selected = map[int]bool{}
								_ = manager.SaveProfile(settings, mods)
								status = "Mod deleted"
								break
							}
						}
						if showIcons {
							imgui.SetCursorPosY(rowY + iconSize)
						}
						imgui.Dummy(imgui.NewVec2(0, 4))
					}
					imgui.EndChild()
				}
				if orderChanged {
					releaseModIcons(icons)
					mods, problems, _ = manager.Load(settings)
					_ = manager.LoadProfile(settings, mods)
					icons = loadModIcons(mods)
					selected = map[int]bool{}
					orderChanged = false
				}
				if detailIndex >= 0 && detailIndex < len(mods) && detailsVisible {
					if detailInfoIndex != detailIndex {
						detailSizeBytes, detailModified = modDiskInfo(mods[detailIndex].Directory)
						detailInfoIndex = detailIndex
					}
					if detailSizePending {
						imgui.SetNextWindowSizeV(imgui.NewVec2(620, 560), imgui.CondAppearing)
						detailSizePending = false
					}
					// Keep the detail editor above the main content, like a dialog,
					// without creating a second native window.
					if detailFocusPending {
						imgui.SetNextWindowFocus()
						detailFocusPending = false
					}
					if imgui.BeginV(tr(language, "Mod details"), &detailsVisible, imgui.WindowFlagsNoCollapse|imgui.WindowFlagsNoSavedSettings) {
						mod := mods[detailIndex]
						if detailIndex < len(icons) && icons[detailIndex] != nil {
							icon := placeholderIcon
							if detailIndex < len(icons) && icons[detailIndex] != nil {
								icon = icons[detailIndex]
							}
							if showIcons {
								imgui.Image(icon.ID, imgui.NewVec2(128*fontScale*iconScale, 128*fontScale*iconScale))
							}
							imgui.SameLine()
						}
						imgui.TextWrapped(mod.Manifest.Name)
						imgui.Text(fmt.Sprintf("%s: %s", tr(language, "Modified"), detailModified.Format("2006-01-02 15:04:05")))
						imgui.Text(fmt.Sprintf("%s: %s", tr(language, "Size"), formatModSize(detailSizeBytes)))
						if strings.TrimSpace(mod.Manifest.Description) != "" || len(mod.Manifest.Options) > 1 {
							// Keep metadata separate from the content that follows it.
							detailOptionSeparator()
							imgui.Spacing()
						}
						imgui.TextWrapped(mod.Manifest.Description)
						if len(mod.Manifest.Options) > 1 && mod.Manifest.Version == -1 {
							// Separate the mod description from its selectable options.
							if strings.TrimSpace(mod.Manifest.Description) != "" {
								detailOptionSeparator()
								imgui.Spacing()
							}
							choice := 0
							if len(mod.SelectedOptions) > 0 {
								choice = mod.SelectedOptions[0]
							}
							if choice < 0 || choice >= len(mod.Manifest.Options) {
								choice = 0
							}
							imgui.Spacing()
							if imgui.BeginCombo("Legacy option", mod.Manifest.Options[choice].Name) {
								for j, option := range mod.Manifest.Options {
									if imgui.SelectableBoolV(option.Name, j == choice, 0, imgui.NewVec2(0, 0)) {
										mod.SelectedOptions[0] = j
									}
								}
								imgui.EndCombo()
							}
							imgui.Spacing()
						} else if len(mod.Manifest.Options) > 1 {
							// Options are shown only when there is more than one choice;
							// therefore a mod without options has no trailing separator.
							if strings.TrimSpace(mod.Manifest.Description) != "" {
								detailOptionSeparator()
								imgui.Spacing()
							}
							for i, option := range mod.Manifest.Options {
								if i > 0 {
									detailOptionSeparator()
									imgui.Spacing()
								}
								if i < len(mod.EnabledOptions) {
									imgui.Checkbox(fmt.Sprintf("%s##option-%d", option.Name, i), &mod.EnabledOptions[i])
								}
								if option.Description != "" {
									imgui.TextWrapped(option.Description)
								}
								if len(option.SubOptions) > 0 && i < len(mod.SelectedOptions) {
									choice := mod.SelectedOptions[i]
									if choice < 0 || choice >= len(option.SubOptions) {
										choice = 0
										mod.SelectedOptions[i] = 0
									}
									preview := option.SubOptions[choice].Name
									if imgui.BeginCombo(fmt.Sprintf("%s choice##sub-%d", option.Name, i), preview) {
										for j, sub := range option.SubOptions {
											if imgui.SelectableBoolV(sub.Name, j == choice, 0, imgui.NewVec2(0, 0)) {
												mod.SelectedOptions[i] = j
											}
										}
										imgui.EndCombo()
									}
									imgui.TextWrapped(option.SubOptions[mod.SelectedOptions[i]].Description)
								}
							}
						}
						if len(mod.Manifest.Options) > 1 && imgui.Button(tr(language, "Save option changes")) {
							if err := manager.SaveProfile(settings, mods); err != nil {
								status = err.Error()
							} else {
								status = "Option changes saved"
								detailsVisible = false
							}
						}
					}
					imgui.End()
				}
				imgui.EndTabItem()
			}
			if imgui.BeginTabItem(tr(language, "Install")) {
				imgui.Text(tr(language, "Install ZIP mods into") + ": " + filepath.Join(settings.StorageDir, "Mods"))
				if imgui.Button(tr(language, "Choose ZIP files...")) {
					if paths, err := selectArchives(filepath.Join(settings.StorageDir, "Mods")); err == nil {
						known := make(map[string]bool, len(archivePaths))
						for _, path := range archivePaths {
							known[filepath.Clean(path)] = true
						}
						for _, path := range paths {
							path = filepath.Clean(path)
							if !known[path] {
								archivePaths = append(archivePaths, path)
								known[path] = true
							}
						}
						status = fmt.Sprintf("Selected %d archive(s)", len(archivePaths))
					}
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Clear selected files")) {
					archivePaths = nil
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Install selected mods")) && !installBusy {
					if len(archivePaths) == 0 {
						status = tr(language, "Choose at least one ZIP file")
					} else {
						installBusy = true
						installResultCh = importArchivesParallel(settings, append([]string(nil), archivePaths...))
					}
				}
				imgui.SameLine()
				imgui.Text(fmt.Sprintf("%d %s", len(archivePaths), tr(language, "archives selected")))
				if installBusy {
					imgui.SameLine()
					imgui.Text(tr(language, "Processing..."))
				}
				if len(archivePaths) > 0 {
					listHeight := imgui.ContentRegionAvail().Y - 150
					if listHeight < 100 {
						listHeight = 100
					}
					if imgui.BeginChildStrV("selected-archives", imgui.NewVec2(0, listHeight), imgui.ChildFlagsBorders, 0) {
						removeArchive := -1
						for i, archive := range archivePaths {
							imgui.Text(fmt.Sprintf("%d. %s", i+1, filepath.Base(archive)))
							imgui.SameLine()
							if imgui.Button(fmt.Sprintf("%s##archive-%d", tr(language, "Remove file"), i)) {
								removeArchive = i
							}
						}
						if removeArchive >= 0 {
							archivePaths = append(archivePaths[:removeArchive], archivePaths[removeArchive+1:]...)
						}
						imgui.EndChild()
					}
				}
				imgui.TextWrapped(tr(language, "Windows picker help"))
				imgui.Separator()
				if imgui.Button(tr(language, "Deploy enabled mods")) && !deployBusy {
					_ = manager.SaveProfile(settings, mods)
					deployDone, deployTotal = 0, 0
					deployBusy = true
					deployProgressCh = deployAsync(settings, append([]*manager.Mod(nil), mods...))
				}
				if deployBusy {
					imgui.SameLine()
					fraction := float32(0)
					if deployTotal > 0 {
						fraction = float32(deployDone) / float32(deployTotal)
					}
					imgui.ProgressBarV(fraction, imgui.NewVec2(220, 0), fmt.Sprintf("%d/%d", deployDone, deployTotal))
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Purge deployed files")) {
					if err := manager.Purge(settings); err != nil {
						status = err.Error()
					} else {
						status = tr(language, "Deployment purged")
					}
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Launch HD2")) {
					if err := manager.LaunchGame(settings); err != nil {
						status = err.Error()
					} else {
						status = "HD2 launch requested"
					}
				}
				if status != "" {
					imgui.TextWrapped(status)
				}
				imgui.EndTabItem()
			}
			if imgui.BeginTabItem(tr(language, "Test")) {
				checkUI.draw(language, settings, mods, problems, installBusy || deployBusy || replaceBusy)
				imgui.EndTabItem()
			}
			if imgui.BeginTabItem(tr(language, "Configuration")) {
				imgui.Text(tr(language, "Paths"))
				gamePath, storagePath := settings.GameDir, settings.StorageDir
				imgui.InputTextWithHint(tr(language, "Game directory"), "path to Helldivers 2", &gamePath, 0, nil)
				updateIMEPosition()
				imgui.InputTextWithHint(tr(language, "Storage directory"), "path to manager data", &storagePath, 0, nil)
				updateIMEPosition()
				if imgui.Button(tr(language, "Apply paths")) {
					settings.GameDir, settings.StorageDir = filepath.Clean(gamePath), filepath.Clean(storagePath)
					status = "Paths updated"
				}
				imgui.Separator()
				imgui.Text(tr(language, "Appearance"))
				if imgui.Checkbox(tr(language, "Show mod icons"), &showIcons) {
					saveUIConfig(settings.StorageDir, fontScale, iconScale, language, showIcons)
				}
				imgui.SliderFloat(tr(language, "Font scale"), &fontScale, 1.0, 3.0)
				if imgui.SliderFloat(tr(language, "Icon scale"), &iconScale, 0.5, 2.0) {
					saveUIConfig(settings.StorageDir, fontScale, iconScale, language, showIcons)
				}
				if imgui.Button(tr(language, "Reset font scale")) {
					fontScale = 1.0
				}
				imgui.Text(tr(language, "Language"))
				languageLabel := map[string]string{"en": "English", "zh-CN": "简体中文"}[language]
				if imgui.BeginCombo("##language", languageLabel) {
					for _, choice := range []string{"en", "zh-CN"} {
						label := map[string]string{"en": "English", "zh-CN": "简体中文"}[choice]
						if imgui.SelectableBoolV(label, language == choice, 0, imgui.NewVec2(0, 0)) {
							language = choice
							saveUIConfig(settings.StorageDir, fontScale, iconScale, language, showIcons)
						}
					}
					imgui.EndCombo()
				}
				imgui.EndTabItem()
			}
			imgui.EndTabBar()
		}
		if replaceVisible {
			imgui.SetNextWindowFocus()
			textSize := imgui.CalcTextSizeV(replaceName, false, 0)
			popupWidth := textSize.X + 100
			if popupWidth < 520 {
				popupWidth = 520
			}
			if popupWidth > 900 {
				popupWidth = 900
			}
			popupHeight := float32(210 + 28*strings.Count(replaceName, "\n"))
			if popupHeight > 520 {
				popupHeight = 520
			}
			imgui.SetNextWindowSizeV(imgui.NewVec2(popupWidth, popupHeight), imgui.CondAppearing)
			if imgui.BeginV(tr(language, "Replace existing mod?"), &replaceVisible, imgui.WindowFlagsNoCollapse|imgui.WindowFlagsNoSavedSettings) {
				imgui.Text(tr(language, "Conflict with"))
				imgui.SameLine()
				imgui.TextColored(imgui.NewVec4(0.35, 0.75, 1.0, 1.0), replaceName)
				if replaceBusy {
					imgui.Text(tr(language, "Processing..."))
				} else {
					imgui.Text(tr(language, "Replace this mod?"))
				}
				if !replaceBusy && imgui.Button(tr(language, "Replace")) {
					replaceBusy = true
					replaceResultCh = replaceAsync(settings, replaceArchive)
				}
				imgui.SameLine()
				if !replaceBusy && imgui.Button(tr(language, "Cancel")) {
					replaceVisible = false
					status = "Import cancelled"
				}
				imgui.End()
			}
		}
		if errorVisible {
			imgui.SetNextWindowFocus()
			imgui.SetNextWindowSizeV(imgui.NewVec2(520, 180), imgui.CondAppearing)
			if imgui.BeginV(tr(language, "Import error"), &errorVisible, imgui.WindowFlagsNoCollapse|imgui.WindowFlagsNoSavedSettings) {
				imgui.TextWrapped(errorMessage)
				if imgui.Button(tr(language, "OK")) {
					errorVisible = false
				}
				imgui.End()
			}
		}
		if categoryVisible {
			imgui.SetNextWindowFocus()
			imgui.SetNextWindowSizeV(imgui.NewVec2(520, 220), imgui.CondAppearing)
			if imgui.BeginV(tr(language, "Create category"), &categoryVisible, imgui.WindowFlagsNoCollapse|imgui.WindowFlagsNoSavedSettings) {
				imgui.InputTextWithHint("##category-name", tr(language, "Category name"), &categoryName, imgui.InputTextFlagsNone, nil)
				updateIMEPosition()
				if imgui.Button(tr(language, "Create category")) {
					name := strings.TrimSpace(categoryName)
					if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
						errorMessage = tr(language, "Category name") + ": invalid name"
						errorVisible = true
					} else if err := os.MkdirAll(filepath.Join(settings.StorageDir, "Mods", name), 0755); err != nil {
						errorMessage = err.Error()
						errorVisible = true
					} else if err := os.WriteFile(filepath.Join(settings.StorageDir, "Mods", name, ".category"), []byte("category\n"), 0644); err != nil {
						errorMessage = err.Error()
						errorVisible = true
					} else {
						categoryVisible = false
						status = tr(language, "Category created")
					}
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Cancel")) {
					categoryVisible = false
				}
				imgui.End()
			}
		}
		if deleteCategoryVisible {
			imgui.SetNextWindowFocus()
			imgui.SetNextWindowSizeV(imgui.NewVec2(520, 180), imgui.CondAppearing)
			if imgui.BeginV(tr(language, "Delete category"), &deleteCategoryVisible, imgui.WindowFlagsNoCollapse|imgui.WindowFlagsNoSavedSettings) {
				imgui.TextWrapped(tr(language, "Category delete notice"))
				if imgui.Button(tr(language, "Delete category")) {
					categoryPath := filepath.Join(settings.StorageDir, "Mods", deleteCategoryName)
					root := filepath.Join(settings.StorageDir, "Mods")
					errFound := ""
					if entries, err := os.ReadDir(categoryPath); err != nil {
						errFound = err.Error()
					} else {
						for _, entry := range entries {
							if entry.Name() == ".category" {
								continue
							}
							if _, err := os.Stat(filepath.Join(root, entry.Name())); err == nil {
								errFound = fmt.Sprintf("%s already exists", entry.Name())
								break
							}
						}
						if errFound == "" {
							for _, entry := range entries {
								if entry.Name() == ".category" {
									continue
								}
								if err := os.Rename(filepath.Join(categoryPath, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
									errFound = err.Error()
									break
								}
							}
						}
					}
					if errFound == "" {
						if err := os.RemoveAll(categoryPath); err != nil {
							errFound = err.Error()
						}
					}
					if errFound != "" {
						errorMessage = errFound
						errorVisible = true
					} else {
						activeCategory = ""
						deleteCategoryVisible = false
						releaseModIcons(icons)
						mods, problems, _ = manager.Load(settings)
						_ = manager.LoadProfile(settings, mods)
						icons = loadModIcons(mods)
						selected = map[int]bool{}
					}
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Cancel")) {
					deleteCategoryVisible = false
				}
				imgui.End()
			}
		}
		if moveVisible {
			imgui.SetNextWindowFocus()
			imgui.SetNextWindowSizeV(imgui.NewVec2(520, 360), imgui.CondAppearing)
			if imgui.BeginV(tr(language, "Move to category"), &moveVisible, imgui.WindowFlagsNoCollapse|imgui.WindowFlagsNoSavedSettings) {
				categories := categoryFolders(settings.StorageDir)
				if len(categories) == 0 {
					imgui.TextWrapped(tr(language, "Create a category first"))
				} else {
					imgui.Text(tr(language, "Select category"))
					if imgui.BeginChildStrV("category-list", imgui.NewVec2(0, 220), imgui.ChildFlagsBorders, 0) {
						for _, category := range categories {
							if imgui.SelectableBoolV(category, moveCategory == category, 0, imgui.NewVec2(0, 0)) {
								moveCategory = category
							}
						}
						imgui.EndChild()
					}
					if imgui.Button(tr(language, "Move")) && moveCategory != "" {
						categoryPath := filepath.Join(settings.StorageDir, "Mods", moveCategory)
						_ = os.WriteFile(filepath.Join(categoryPath, ".category"), []byte("category\n"), 0644)
						errFound := ""
						for i, mod := range mods {
							if !selected[i] {
								continue
							}
							src := mod.Directory
							name := filepath.Base(src)
							if mod.ArchivePath != "" {
								src = mod.ArchivePath
								name = filepath.Base(src)
							}
							dst := filepath.Join(settings.StorageDir, "Mods", moveCategory, name)
							if _, err := os.Stat(dst); err == nil {
								errFound = fmt.Sprintf("%s already exists", name)
								break
							}
							if err := os.Rename(src, dst); err != nil {
								errFound = err.Error()
								break
							}
						}
						if errFound != "" {
							errorMessage = errFound
							errorVisible = true
						} else {
							releaseModIcons(icons)
							mods, problems, _ = manager.Load(settings)
							_ = manager.LoadProfile(settings, mods)
							icons = loadModIcons(mods)
							selected = map[int]bool{}
							moveVisible = false
						}
					}
				}
				imgui.SameLine()
				if imgui.Button(tr(language, "Cancel")) {
					moveVisible = false
				}
				imgui.End()
			}
		}
		imgui.End()
	})
	if placeholderIcon != nil {
		placeholderIcon.Release()
	}
	saveUIConfig(settings.StorageDir, fontScale, iconScale, language, showIcons)
	releaseModIcons(icons)
	return nil
}
