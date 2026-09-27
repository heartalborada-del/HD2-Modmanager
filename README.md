# HD2 Mod Manager (Go + cimgui-go)

This is a native Go implementation of the workflow in [Helldivers2ModManager](https://github.com/teutinsa/Helldivers2ModManager). The reusable manager package scans `storage/Mods`, reads legacy and v1 manifests, persists `enabled.json`, imports ZIP archives, purges deployed patches, and deploys enabled patch triplets into `<game>/data`.

## Run

```powershell
go run ./cmd/hd2mm -game "C:\Program Files (x86)\Steam\steamapps\common\Helldivers 2"
go run ./cmd/hd2mm -game "...\Helldivers 2" -deploy
go run ./cmd/hd2mm -game "...\Helldivers 2" -purge
```

Start the cimgui-go desktop window with the GLFW/OpenGL backend:

```powershell
go run -tags gui ./cmd/hd2mm -gui
# Optional game and storage paths:
go run -tags gui ./cmd/hd2mm -gui -game "D:\Steam\steamapps\common\Helldivers 2" -storage ".\mods-storage"
```

Omit `-game` to scan standard Steam folders and Steam `libraryfolders.vdf` entries automatically. Unless `-storage` is supplied, `Mods` and `enabled.json` are stored beside `hd2mm.exe`.

Requires Go 1.24+ and a compatible C/C++ compiler with CGO enabled. The first GUI build compiles the cimgui bindings and can take several minutes. The GUI is a single window with Mods, Install, Configuration, and Deploy tabs. It uses an anti-aliased vector sans-serif font, includes live font scaling, opens a native Windows multi-select ZIP picker and imports selected files into `Mods`, edits game/storage paths, and saves/loads `enabled.json`.

## Safety

Archive extraction rejects path traversal. Deployment always purges files matching Helldivers patch naming before writing the selected profile, and only reads mod content from the configured `Mods` directory.

