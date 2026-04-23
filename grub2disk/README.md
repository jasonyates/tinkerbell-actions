```
quay.io/tinkerbell/actions/grub2disk:latest
```

The `grub2disk` action chroots into a freshly-provisioned root filesystem and installs GRUB. It supports two modes, selected by the `MODE` env var:

- `MODE=bios` (default) — runs `grub-install <GRUB_DISK>` + `grub-mkconfig`. This is the original behaviour and the env contract is unchanged.
- `MODE=efi` — mounts `EFI_PARTITION` at `/boot/efi`, runs `grub-install --target=x86_64-efi` with `BOOTLOADER_ID`, copies the shim (or `grubx64.efi` if no shim is present) into `/EFI/BOOT/BOOTX64.EFI` for the firmware fallback path, and best-effort registers an NVRAM boot entry via `efibootmgr`.

As with `chroot`, the action requires `/dev` and `/sys` bind-mounted from the host.

## MODE=bios

| Env var             | Required | Example     | Notes                                           |
| ------------------- | -------- | ----------- | ----------------------------------------------- |
| `GRUB_INSTALL_PATH` | yes      | `/dev/sda`  | Block device to chroot into (root filesystem).  |
| `GRUB_DISK`         | yes      | `/dev/sda1` | Disk or partition `grub-install` writes to.     |
| `FS_TYPE`           | yes      | `ext4`      | Filesystem type of `GRUB_INSTALL_PATH`.         |
| `MODE`              | no       | `bios`      | Defaults to `bios` if unset.                    |

```yaml
- name: "grub_2_disk"
  image: quay.io/tinkerbell/actions/grub2disk:latest
  timeout: 180
  environment:
    GRUB_INSTALL_PATH: /dev/sda
    GRUB_DISK: /dev/sda1
    FS_TYPE: ext4
```

## MODE=efi

| Env var             | Required | Example     | Notes                                                                          |
| ------------------- | -------- | ----------- | ------------------------------------------------------------------------------ |
| `MODE`              | yes      | `efi`       | Must be set to `efi`.                                                          |
| `GRUB_INSTALL_PATH` | yes      | `/dev/md0`  | Block device to chroot into (root filesystem, e.g. an mdadm RAID or LV).       |
| `FS_TYPE`           | yes      | `ext4`      | Filesystem type of `GRUB_INSTALL_PATH`.                                        |
| `EFI_PARTITION`     | yes      | `/dev/sda1` | ESP block device; mounted at `/boot/efi` inside the chroot.                    |
| `BOOTLOADER_ID`     | no       | `ubuntu`    | `grub-install --bootloader-id` and NVRAM label. Defaults to `ubuntu`.          |
| `REGISTER_NVRAM`    | no       | `true`      | `true`/`false`. Defaults to `true`. Best-effort — failures only log a warning. |

Call the action once per ESP (typically twice for a RAID1 boot mirror, with different `EFI_PARTITION` + `BOOTLOADER_ID` values):

```yaml
- name: "grub_efi_sda"
  image: quay.io/tinkerbell/actions/grub2disk:latest
  timeout: 180
  environment:
    MODE: efi
    GRUB_INSTALL_PATH: /dev/md0
    FS_TYPE: ext4
    EFI_PARTITION: /dev/sda1
    BOOTLOADER_ID: ubuntu

- name: "grub_efi_sdb"
  image: quay.io/tinkerbell/actions/grub2disk:latest
  timeout: 180
  environment:
    MODE: efi
    GRUB_INSTALL_PATH: /dev/md0
    FS_TYPE: ext4
    EFI_PARTITION: /dev/sdb1
    BOOTLOADER_ID: ubuntu-backup
```

After BIOS-mode install, rebooting drops you into a grub menu like these:

![sample grub 1](sample_grub_menu_1.png)

![sample grub 2](sample_grub_menu_2.png)
