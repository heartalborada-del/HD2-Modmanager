package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"hd2modmanager/internal/manager"
)

func main() {
	storageDir := ""
	if exe, err := os.Executable(); err == nil {
		storageDir = filepath.Dir(exe)
	}
	if storageDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			storageDir = filepath.Join(cwd, "config")
		}
	}
	game := flag.String("game", "", "Helldivers 2 installation directory")
	storage := flag.String("storage", storageDir, "manager data directory (default: folder next to the executable)")
	purge := flag.Bool("purge", false, "remove deployed patch files")
	deploy := flag.Bool("deploy", false, "deploy enabled mods")
	gui := flag.Bool("gui", false, "open the desktop interface (requires -tags gui)")
	flag.Parse()
	if *game == "" {
		*game = manager.FindGameDirectory()
	}
	if *gui || (!*deploy && !*purge) {
		_ = manager.CleanupTemp(manager.NewSettings(*game, *storage))
		if err := runGUI(manager.NewSettings(*game, *storage)); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *game == "" {
		fmt.Println("Helldivers 2 was not found automatically; pass -game <directory>")
		return
	}
	if *game == "" {
		fmt.Println("HD2 Mod Manager\nUsage: hd2mm -game <Helldivers2> [-deploy|-purge]")
		return
	}
	s := manager.NewSettings(*game, *storage)
	_ = manager.CleanupTemp(s)
	mods, problems, err := manager.Load(s)
	if err != nil {
		log.Fatal(err)
	}
	if err := manager.LoadProfile(s, mods); err != nil {
		log.Printf("profile: %v", err)
	}
	fmt.Printf("Loaded %d mods\n", len(mods))
	for _, p := range problems {
		fmt.Printf("%s: %s (%s)\n", p.Kind, p.Directory, p.Detail)
	}
	if *purge {
		if err := manager.Purge(s); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Deployment purged")
	}
	if *deploy {
		if err := manager.Deploy(s, mods); err != nil {
			log.Fatal(err)
		}
		if err := manager.SaveProfile(s, mods); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Mods deployed")
	}
}
