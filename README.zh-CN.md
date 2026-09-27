# HD2 Mod Manager

[English](README.md)

HD2 Mod Manager 是一个使用 Go 和 [cimgui-go](https://github.com/AllenDang/cimgui-go) 编写的《HELLDIVERS 2》原生模组管理器。它可以扫描已安装的模组，管理启用状态和置底加载顺序，导入常见压缩包，部署选中的补丁文件，并通过二分启动测试协助定位有问题的模组。

本项目参考了 [Helldivers2ModManager](https://github.com/teutinsa/Helldivers2ModManager) 的工作流程。

## 功能

- 支持传统和新版模组清单，也支持没有 `manifest.json` 的模组。
- 启用、禁用、删除、排序模组，并将模组置底。
- 创建分类并将模组移动到自定义分类中。
- 导入 ZIP、7z、RAR、TAR、GZ 等常见压缩格式。
- 检测重复模组 ID，并在替换已有模组前进行确认。
- 部署已启用模组，或清除之前部署的文件。
- 搜索模组，查看介绍、文件大小和修改日期。
- 支持英文和简体中文界面。
- 支持设置超时时间的启动二分测试，用于定位可能导致游戏启动失败的模组。

## 环境要求

- Windows
- Go 1.25 或更高版本
- 构建 GUI 需要支持 CGO 的 C/C++ 编译器

第一次构建 GUI 时需要编译 cimgui 绑定，可能需要几分钟。

## 构建

构建命令行版本：

```powershell
go build -o hd2mm.exe ./cmd/hd2mm
```

构建不显示命令行窗口的 GUI 版本：

```powershell
go build -tags gui -ldflags "-H=windowsgui" -o hd2mm.exe ./cmd/hd2mm
```

## 运行

启动 GUI：

```powershell
.\hd2mm.exe -gui
```

也可以指定游戏目录和模组存储目录：

```powershell
.\hd2mm.exe -gui `
  -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2" `
  -storage ".\Mods"
```

省略 `-game` 时，程序会自动搜索常见 Steam 目录和 Steam 库文件。运行时配置保存在 `config` 目录中。如果没有指定其他路径，模组数据保存在 `Mods` 目录中。

也可以使用命令行执行部署操作：

```powershell
.\hd2mm.exe -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2" -deploy
.\hd2mm.exe -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2" -purge
```

## 安全说明

压缩包解压会拒绝路径穿越。部署前会清理符合《HELLDIVERS 2》补丁命名规则的旧文件，然后写入当前配置中的文件。程序只会读取配置存储目录中的模组内容。

## 许可证

本项目使用 [MIT License](LICENSE) 授权。
