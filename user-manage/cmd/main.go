//go:build linux

package main

import (
	"context"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/actions/pkg/chroot"
	"github.com/tinkerbell/actions/pkg/metadata"
	"github.com/tinkerbell/actions/user-manage/internal/users"
)

const sshdConfig = "/etc/ssh/sshd_config"

func main() {
	log.Infof("user-manage - users + ssh keys + sshd_config")

	c, err := metadata.New()
	if err != nil {
		log.Fatal(err)
	}
	md, err := c.Fetch(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	inst := md.Instance

	if err := chroot.Enter(os.Getenv("BLOCK_DEVICE"), os.Getenv("FS_TYPE"), md.Instance.Storage.Filesystems); err != nil {
		log.Fatal(err)
	}

	// Root password + root authorized_keys (legacy back-compat)
	if inst.CryptedRootPassword != "" {
		if err := users.SetPassword("root", inst.CryptedRootPassword); err != nil {
			log.Fatalf("set root password: %v", err)
		}
		log.Info("root password set from crypted_root_password")
	}
	if err := users.WriteAuthorizedKeys("root", inst.SSHKeys); err != nil {
		log.Fatalf("write root authorized_keys: %v", err)
	}
	if len(inst.SSHKeys) > 0 {
		log.Infof("installed %d ssh key(s) for root", len(inst.SSHKeys))
	}

	// Non-root users
	for _, u := range inst.Users {
		if err := users.EnsureUser(u); err != nil {
			log.Fatalf("ensure %s: %v", u.Username, err)
		}
		if u.CryptedPassword != "" {
			if err := users.SetPassword(u.Username, u.CryptedPassword); err != nil {
				log.Fatalf("set %s password: %v", u.Username, err)
			}
		}
		if err := users.WriteAuthorizedKeys(u.Username, u.SSHAuthorizedKeys); err != nil {
			log.Fatalf("write %s authorized_keys: %v", u.Username, err)
		}
		if u.Sudo {
			if err := users.AddToGroup(u.Username, "sudo"); err != nil {
				log.Fatalf("add %s to sudo: %v", u.Username, err)
			}
		}
		log.Infof("applied user %s (sudo=%v, keys=%d)", u.Username, u.Sudo, len(u.SSHAuthorizedKeys))
	}

	// sshd_config rewrite — global, independent of identity.
	if inst.SSHD != nil {
		in, err := os.ReadFile(sshdConfig)
		if err != nil {
			log.Fatalf("read sshd_config: %v", err)
		}
		out := users.RewriteSSHDConfig(string(in), inst.SSHD)
		if err := os.WriteFile(sshdConfig, []byte(out), 0o644); err != nil {
			log.Fatalf("write sshd_config: %v", err)
		}
		log.Info("sshd_config updated")
	}
}
