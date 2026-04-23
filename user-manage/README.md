# user-manage

Manage users, SSH keys, and sshd_config on an installed OS during
provisioning. Reads metadata from Hegel and applies it inside the
target root:

- `crypted_root_password` → `chpasswd -e root:$HASH`
- `ssh_keys` → `/root/.ssh/authorized_keys`
- `users[]` → `useradd -m`, password, authorized_keys, optional sudo
- `sshd.permit_root_login` / `sshd.password_authentication` → rewritten
  directives in `/etc/ssh/sshd_config`

The action never restarts sshd — the OS hasn't booted yet; directives
take effect on first boot.

## Environment variables

| Var | Required | Notes |
|---|---|---|
| `BLOCK_DEVICE` | yes | Absolute path to the root block device |
| `FS_TYPE` | yes | Filesystem of the root device |
| `MIRROR_HOST` | yes | Hegel host |
| `METADATA_SERVICE_PORT` | no | Default `50061` |

## Metadata schema

```yaml
metadata:
  instance:
    crypted_root_password: "$6$..."
    ssh_keys:
      - "ssh-ed25519 AAAA... root@bastion"

    users:
      - username: ubuntu
        crypted_password: "$6$..."
        ssh_authorized_keys:
          - "ssh-ed25519 AAAA... ubuntu@bastion"
        sudo: true
        shell: /bin/bash

    sshd:
      permit_root_login: prohibit-password
      password_authentication: true
```

## Example workflow step

```yaml
- name: "user-manage"
  image: ghcr.io/jasonyates/tinkerbell-actions/user-manage:latest
  timeout: 120
  environment:
    BLOCK_DEVICE: "/dev/vg0/root"
    FS_TYPE: "ext4"
    METADATA_SERVICE_PORT: "7080"
    MIRROR_HOST: "31.24.228.5"
```
