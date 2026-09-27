# HD2 Mod Manager

[简体中文](README.zh-CN.md)

HD2 Mod Manager is a native Windows mod manager for Helldivers 2, written in Go with [cimgui-go](https://github.com/AllenDang/cimgui-go). It scans installed mods, manages enabled and bottom-pinned load order, imports common archive formats, deploys selected patch files, and can help identify problematic mods through binary startup testing.

The project is based on the workflow of [Helldivers2ModManager](https://github.com/teutinsa/Helldivers2ModManager).

## Features

- Manage legacy and modern mod manifests, including mods without `manifest.json`.
- Enable, disable, delete, reorder, and pin mods to the bottom of the load order.
- Organize mods into user-created categories.
- Import ZIP, 7z, RAR, TAR, GZ, and related archive formats.
- Detect duplicate mod IDs and ask before replacing an existing mod.
- Deploy enabled mods and remove previously deployed files.
- Search mods and view descriptions, file size, and modification date.
- English and Simplified Chinese interface support.
- Optional binary startup testing with a configurable timeout.

## Requirements

- Windows
- Go 1.25 or newer
- A compatible C/C++ compiler with CGO enabled for the GUI build

The first GUI build compiles the cimgui bindings and may take several minutes.

## Build

Build the command-line executable:

```powershell
go build -o hd2mm.exe ./cmd/hd2mm
```

Build the GUI executable without opening a console window:

```powershell
go build -tags gui -ldflags "-H=windowsgui" -o hd2mm.exe ./cmd/hd2mm
```

## Run

Start the GUI:

```powershell
.\hd2mm.exe -gui
```

You can provide the game directory and mod storage directory explicitly:

```powershell
.\hd2mm.exe -gui `
  -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2" `
  -storage ".\Mods"
```

Without `-game`, the manager searches standard Steam locations and Steam library entries. Runtime settings are stored in the `config` directory. Mod data is stored in `Mods` unless another storage path is supplied.

The command-line deployment operations are also available:

```powershell
.\hd2mm.exe -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2" -deploy
.\hd2mm.exe -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2" -purge
```

## Safety

Archive extraction rejects path traversal. Deployment removes files matching Helldivers 2 patch naming before writing the selected profile, and reads mod content only from the configured storage directory.

## License

This project is licensed under the [MIT License](LICENSE).
