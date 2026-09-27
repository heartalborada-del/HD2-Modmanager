//go:build gui

package main

import (
	"fmt"
	"github.com/AllenDang/cimgui-go/imgui"
	"hd2modmanager/internal/manager"
	"strings"
)

type modCheckUpdate struct {
	done, total int
	finished    bool
	issues      []manager.CheckIssue
}

type modCheckUI struct {
	updates          chan modCheckUpdate
	result           modCheckUpdate
	completed        bool
	startupBusy      bool
	startupResult    string
	thresholdSeconds int32
	startupEvents    chan manager.BinaryTestStep
	startupStatus    map[string]int // 0 pending, 1 passed, 2 failed
}

func init() {
	for key, pair := range map[string][2]string{
		"Test":                      {"Test", "测试"},
		"Check mods":                {"Check mods", "检查模组"},
		"Check help":                {"Checks current option files for all mods\nShared targets among enabled mods are warnings; in-game crashes cannot be detected by this check.", "检查所有模组当前选项的文件\n已启用模组的相同补丁目标会被提示，但不一定存在故障；此检查无法判断游戏内崩溃"},
		"Check summary":             {"Checked %d mods, %d findings", "已检查 %d 个模组，发现 %d 项提示"},
		"Archive check unavailable": {"Archive not checked: import as an extracted mod first", "压缩包未检查：请先重新导入为解压后的模组"},
		"Cannot read option folder": {"Cannot read option folder", "无法读取选项文件夹"},
		"Cannot read patch":         {"Cannot read patch", "无法读取补丁"},
		"Empty patch file":          {"Empty patch file", "补丁文件为空"},
		"Missing base patch":        {"Missing base patch", "缺少配套的基础补丁"},
		"Shared patch target":       {"Shared patch target (check load order)", "与其他已启用模组使用相同补丁目标（请检查加载顺序）"},
		"No selected patch files":   {"No deployable patches in current options", "当前选项下没有可部署的补丁"},
	} {
		translations[key] = pair
	}
	translations["Startup timeout (seconds)"] = [2]string{"Startup timeout (seconds)", "启动超时时间（秒）"}
	translations["Test startup"] = [2]string{"Test startup", "测试启动"}
	translations["Startup test running"] = [2]string{"Starting HD2 and watching for an early exit...", "正在启动 HD2 并监控是否立即退出..."}
	translations["Startup passed"] = [2]string{"HD2 stayed running during startup", "HD2 启动阶段未发现异常退出"}
	translations["Startup crash"] = [2]string{"HD2 exited during startup; this may indicate a mod crash", "HD2 在启动阶段退出，可能存在模组冲突"}
	translations["Startup not observed"] = [2]string{"HD2 process was not observed; Steam may still be starting it", "未检测到 HD2 进程，Steam 可能仍在启动"}
}

func (state *modCheckUI) draw(language string, settings manager.Settings, mods []*manager.Mod, problems []manager.Problem, busy bool) {
	if state.thresholdSeconds == 0 {
		state.thresholdSeconds = 60
	}
	if state.startupEvents != nil {
		for {
			select {
			case step, ok := <-state.startupEvents:
				if !ok {
					state.startupEvents = nil
					break
				}
				for _, name := range step.Names {
					if step.Crashed {
						state.startupStatus[name] = 2
					} else if state.startupStatus[name] != 2 {
						state.startupStatus[name] = 1
					}
				}
			default:
				goto startupEventsDone
			}
		}
	}
startupEventsDone:
	if state.updates != nil {
		select {
		case update := <-state.updates:
			state.result = update
			if update.finished {
				state.updates = nil
				state.completed = true
			}
		default:
		}
	}
	imgui.TextWrapped(tr(language, "Check help"))
	imgui.BeginDisabledV(busy || state.updates != nil)
	if imgui.Button(tr(language, "Check mods")) {
		// Workers get independent option state; changing the UI cannot race checks.
		snapshot := make([]*manager.Mod, len(mods))
		for i, mod := range mods {
			clone := *mod
			clone.SelectedOptions = append([]int(nil), mod.SelectedOptions...)
			clone.EnabledOptions = append([]bool(nil), mod.EnabledOptions...)
			snapshot[i] = &clone
		}
		ch := make(chan modCheckUpdate, 1)
		state.updates = ch
		state.completed = false
		state.result = modCheckUpdate{total: len(mods)}
		scanProblems := append([]manager.Problem(nil), problems...)
		go func() {
			issues := manager.CheckMods(snapshot, func(done, total int) {
				select {
				case ch <- modCheckUpdate{done: done, total: total}:
				default:
				}
			})
			for _, p := range scanProblems {
				issues = append(issues, manager.CheckIssue{Mod: p.Directory, Kind: p.Kind, Detail: p.Detail})
			}
			ch <- modCheckUpdate{done: len(snapshot), total: len(snapshot), finished: true, issues: issues}
		}()
	}
	imgui.EndDisabled()
	imgui.Separator()
	imgui.BeginDisabledV(busy || state.updates != nil || state.startupBusy)
	imgui.SliderIntV(tr(language, "Startup timeout (seconds)"), &state.thresholdSeconds, 10, 300, "%d", 0)
	if imgui.Button(tr(language, "Test startup")) {
		state.startupBusy = true
		state.startupResult = tr(language, "Startup test running")
		state.startupStatus = make(map[string]int, len(mods))
		for _, mod := range mods {
			if mod.Enabled {
				state.startupStatus[mod.Manifest.Name] = 0
			}
		}
		events := make(chan manager.BinaryTestStep, 32)
		state.startupEvents = events
		go func() {
			snapshot := make([]*manager.Mod, len(mods))
			for i, mod := range mods {
				clone := *mod
				clone.SelectedOptions = append([]int(nil), mod.SelectedOptions...)
				clone.EnabledOptions = append([]bool(nil), mod.EnabledOptions...)
				snapshot[i] = &clone
			}
			found, err := manager.BinaryStartupTest(settings, snapshot, int(state.thresholdSeconds), func(message string) { state.startupResult = message }, func(step manager.BinaryTestStep) { events <- step })
			close(events)
			if err != nil {
				state.startupResult = err.Error()
			} else if len(found) > 0 {
				state.startupResult = tr(language, "Startup crash") + ": " + strings.Join(found, ", ")
			} else {
				state.startupResult = tr(language, "Startup passed")
			}
			state.startupBusy = false
		}()
	}
	imgui.EndDisabled()
	if state.startupResult != "" {
		imgui.TextWrapped(state.startupResult)
	}
	if len(state.startupStatus) > 0 {
		imgui.Separator()
		for _, mod := range mods {
			if !mod.Enabled {
				continue
			}
			color := imgui.NewVec4(0.55, 0.55, 0.55, 1)
			switch state.startupStatus[mod.Manifest.Name] {
			case 1:
				color = imgui.NewVec4(0.3, 0.9, 0.35, 1)
			case 2:
				color = imgui.NewVec4(1, 0.25, 0.25, 1)
			}
			imgui.TextColored(color, fmt.Sprintf("%s", mod.Manifest.Name))
		}
	}
	if state.updates != nil {
		fraction := float32(0)
		if state.result.total > 0 {
			fraction = float32(state.result.done) / float32(state.result.total)
		}
		imgui.ProgressBarV(fraction, imgui.NewVec2(-1, 0), tr(language, "Processing..."))
	}
	if state.completed {
		imgui.Text(fmt.Sprintf(tr(language, "Check summary"), state.result.total, len(state.result.issues)))
	}
	resultHeight := imgui.ContentRegionAvail().Y
	if resultHeight < 80 {
		resultHeight = 80
	}
	if imgui.BeginChildStrV("check-results", imgui.NewVec2(0, resultHeight), imgui.ChildFlagsNone, imgui.WindowFlagsNone) {
		for _, issue := range state.result.issues {
			imgui.TextWrapped(issue.Mod)
			imgui.TextWrapped(tr(language, issue.Kind))
			imgui.TextWrapped(issue.Detail)
			imgui.Separator()
		}
	}
	imgui.EndChild()
}
