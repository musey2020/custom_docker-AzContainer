// Package main — azcontainer CLI + daemon.
package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"azcontainer/internal/daemon"
	"azcontainer/internal/image"
	"azcontainer/internal/runtime"
	"azcontainer/internal/spec"
	"azcontainer/internal/state"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	// — Daemon —
	case "daemon":
		err = daemon.Serve()
	case "init":
		// Daxili child re-exec; daemon istifadə etmir.
		err = runtime.Init(args)
	// — Container management (daemon vasitəsilə) —
	case "run":
		err = cmdRun(args)
	case "ps", "list":
		err = cmdList()
	case "stop":
		err = cmdStop(args)
	case "rm":
		err = cmdRemove(args)
	case "logs":
		err = cmdLogs(args)
	case "stats":
		err = cmdStats(args)

	// — Image management (lokal, daemon yox) —
	case "pull":
		err = cmdPull(args)
	case "import":
		err = cmdImport(args)
	case "import-rootfs":
		err = cmdImportRootfs(args)
	case "images":
		err = cmdImages()
	case "spec":
		err = cmdSpec(args)

	// — Köhnə direct run (daemon olmadan) —
	case "run-direct":
		err = runtime.Run(args)

	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s uğursuz: %v\n", cmd, err)
		os.Exit(1)
	}
}

// — Daemon vasitəsilə komandalar —

func cmdRun(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: run <image> [cmd...]")
	}

	cli, err := daemon.Dial()
	if err != nil {
		return err
	}
	defer cli.Close()

	imageName := args[0]
	command := args[1:]

	reply, err := cli.Run(imageName, command)
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", reply.ID[:12])
	if reply.IP != "" {
		fmt.Printf("IP: %s\n", reply.IP)
	}
	return nil
}

func cmdList() error {
	cli, err := daemon.Dial()
	if err != nil {
		return err
	}
	defer cli.Close()

	items, err := cli.List()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tIMAGE\tCOMMAND\tSTATUS\tIP\tCREATED")

	for _, item := range items {
		c, ok := item.(*state.Container)
		if !ok {
			continue
		}
		shortID := c.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		cmd := strings.Join(c.Command, " ")
		if len(cmd) > 30 {
			cmd = cmd[:27] + "..."
		}
		age := time.Since(c.CreatedAt).Round(time.Second)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s ago\n",
			shortID, c.Image, cmd, c.Status, c.IP, age)
	}
	return w.Flush()
}

func cmdStop(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: stop <id>")
	}
	cli, err := daemon.Dial()
	if err != nil {
		return err
	}
	defer cli.Close()

	if err := cli.Stop(args[0]); err != nil {
		return err
	}
	fmt.Println(args[0])
	return nil
}

func cmdRemove(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: rm <id>")
	}
	cli, err := daemon.Dial()
	if err != nil {
		return err
	}
	defer cli.Close()

	if err := cli.Remove(args[0]); err != nil {
		return err
	}
	fmt.Println(args[0])
	return nil
}

func cmdLogs(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: logs <id>")
	}
	cli, err := daemon.Dial()
	if err != nil {
		return err
	}
	defer cli.Close()

	content, err := cli.Logs(args[0])
	if err != nil {
		return err
	}
	fmt.Print(content)
	return nil
}

func cmdStats(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: stats <id> [--watch]")
	}
	cli, err := daemon.Dial()
	if err != nil {
		return err
	}
	defer cli.Close()

	watch := len(args) > 1 && args[1] == "--watch"

	for {
		reply, err := cli.Stats(args[0])
		if err != nil {
			return err
		}
		if watch {
			fmt.Print("\033[H\033[2J") // ekranı təmizlə
		}
		fmt.Printf("Container: %s\n%s\n",
			reply.Snapshot.ContainerID[:12],
			reply.Snapshot.FormatHuman())

		if !watch {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

// — Image komandaları (daemon yox) —

func cmdPull(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: pull <image:tag> [local_name]")
	}
	ref := args[0]
	localName := ref
	if len(args) >= 2 {
		localName = args[1]
	} else {
		for i, c := range ref {
			if c == ':' {
				localName = ref[:i]
				break
			}
		}
	}

	store, err := image.NewStore()
	if err != nil {
		return err
	}
	puller := image.NewPuller(store)
	if err := puller.Pull(ref, localName); err != nil {
		return err
	}
	return store.AssembleRootFS(localName)
}

func cmdImport(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("istifadə: import <tar> <name>")
	}
	store, err := image.NewStore()
	if err != nil {
		return err
	}
	if err := store.ImportDockerSave(args[0], args[1]); err != nil {
		return err
	}
	return store.AssembleRootFS(args[1])
}

func cmdImportRootfs(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("istifadə: import-rootfs <tar.gz> <name>")
	}
	store, err := image.NewStore()
	if err != nil {
		return err
	}
	if err := store.ImportRootfsTar(args[0], args[1]); err != nil {
		return err
	}
	return store.AssembleRootFS(args[1])
}

func cmdImages() error {
	store, err := image.NewStore()
	if err != nil {
		return err
	}
	names, err := store.ListImages()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("(image yoxdur)")
		return nil
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}

func cmdSpec(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("istifadə: spec <image> [output.json]")
	}
	imageName := args[0]
	output := "config.json"
	if len(args) >= 2 {
		output = args[1]
	}

	store, err := image.NewStore()
	if err != nil {
		return err
	}
	_, config, err := store.LoadImage(imageName)
	if err != nil {
		return err
	}
	s := spec.DefaultSpec(config, store.ImageRootFS(imageName), "")
	if err := spec.Save(s, output); err != nil {
		return err
	}
	fmt.Printf("✓ %s\n", output)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "istifadə:")
	fmt.Fprintln(os.Stderr, "  Daemon:")
	fmt.Fprintln(os.Stderr, "    azcontainer daemon                          — daemon-u işə sal")
	fmt.Fprintln(os.Stderr, "  Container:")
	fmt.Fprintln(os.Stderr, "    azcontainer run <image> [cmd...]            — container işə sal")
	fmt.Fprintln(os.Stderr, "    azcontainer ps                              — siyahı")
	fmt.Fprintln(os.Stderr, "    azcontainer stop <id>                       — dayandır")
	fmt.Fprintln(os.Stderr, "    azcontainer rm <id>                         — sil")
	fmt.Fprintln(os.Stderr, "    azcontainer logs <id>                       — log-lar")
	fmt.Fprintln(os.Stderr, "    azcontainer stats <id> [--watch]            — resurs istifadəsi")
	fmt.Fprintln(os.Stderr, "  Image:")
	fmt.Fprintln(os.Stderr, "    azcontainer pull <image:tag>                — Docker Hub-dan")
	fmt.Fprintln(os.Stderr, "    azcontainer import-rootfs <tar.gz> <name>   — lokal rootfs")
	fmt.Fprintln(os.Stderr, "    azcontainer images                          — image siyahısı")
	fmt.Fprintln(os.Stderr, "    azcontainer spec <image> [out]              — config.json yarat")
}
