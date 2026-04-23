//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/actions/pkg/chroot"
	"github.com/tinkerbell/actions/pkg/metadata"
	"github.com/tinkerbell/actions/serial-console/internal/serial"
)

const (
	grubDefaults = "/etc/default/grub.d/50-cloudimg-settings.cfg"
	grubCfg      = "/boot/grub/grub.cfg"
)

func main() {
	log.Infof("serial-console - grub console configurator")

	c, err := metadata.New()
	if err != nil {
		log.Fatal(err)
	}
	md, err := c.Fetch(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if md.Instance.Console == nil || md.Instance.Console.TTY == "" {
		log.Info("serial-console: no Console config in metadata; skipping")
		return
	}

	if err := chroot.Enter(os.Getenv("BLOCK_DEVICE"), os.Getenv("FS_TYPE")); err != nil {
		log.Fatal(err)
	}

	in, err := os.ReadFile(grubDefaults)
	if err != nil {
		log.Fatalf("read %s: %v", grubDefaults, err)
	}
	out := serial.RewriteGrubDefaults(string(in), md.Instance.Console)
	if err := os.WriteFile(grubDefaults, []byte(out), 0o644); err != nil {
		log.Fatalf("write %s: %v", grubDefaults, err)
	}

	mkcfg := exec.Command("grub-mkconfig", "-o", grubCfg)
	mkcfg.Stdout, mkcfg.Stderr = os.Stdout, os.Stderr
	if err := mkcfg.Run(); err != nil {
		log.Fatalf("grub-mkconfig: %v", err)
	}
	if err := os.Chmod(grubCfg, 0o644); err != nil {
		log.Warnf("chmod %s: %v", grubCfg, err)
	}
	log.Infof("serial-console: applied %s @ %d", md.Instance.Console.TTY, md.Instance.Console.Baud)
}
