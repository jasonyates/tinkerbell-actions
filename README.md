# Actions

This repository is a suite of reusable Tinkerbell Actions that are used to compose Tinkerbell Workflows.

| Name | Description |
| --- | --- |
| [archive2disk](/archive2disk/)    | Write archives to a block device |
| [cexec](/cexec/)                  | chroot and execute binaries |
| [grub2disk](/grub2disk/)          | Install GRUB to a block device (BIOS or UEFI with NVRAM + firmware fallback) |
| [image2disk](/image2disk/)        | Write images to a block device |
| [kexec](/kexec/)                  | kexec to a Linux Kernel |
| [oci2disk](/oci2disk/)            | Stream OCI compliant images from a registry and write to a block device |
| [qemuimg2disk](/qemuimg2disk/)    | Stream images and write to a block device |
| [rootio](/rootio/)                | Manage disks (partition, format, fstab, RAID, LVM) from Hegel metadata |
| [serial-console](/serial-console/)| Configure the serial console of the installed OS from Hegel metadata |
| [slurp](/slurp/)                  | Stream a block device to a remote server |
| [syslinux](/syslinux/)            | Install the syslinux bootloader to a block device |
| [user-manage](/user-manage/)      | Create users, install SSH keys, and tune sshd from Hegel metadata |
| [writefile](/writefile/)          | Write a file to a file system on a block device |

## Metadata-aware actions

This fork adds a wave of actions that read their inputs directly from
Tinkerbell's Hegel metadata endpoint (`http://$MIRROR_HOST:$METADATA_SERVICE_PORT/metadata`)
instead of taking everything via environment variables. They share two
packages under [`pkg/`](/pkg/):

- [`pkg/metadata`](/pkg/metadata/) — typed Go client that mirrors tootles'
  `HackInstance` JSON shape exactly. One source of truth for the wire
  format across every action.
- [`pkg/chroot`](/pkg/chroot/) — `chroot.Enter(blockDev, fsType)` that
  mounts the target root at `/mountAction`, bind-mounts `/dev /proc
  /sys`, and chroots. No teardown — action containers are short-lived
  and the kernel reclaims mounts on exit.

The current wave:

| Action | Metadata consumed | Replaces |
| --- | --- | --- |
| [`rootio fstab`](/rootio/)       | `storage.filesystems[]`          | `printf ... > /etc/fstab` cexec blob |
| [`serial-console`](/serial-console/) | `instance.console.{tty, baud}`   | Hand-rolled `sed` on `GRUB_CMDLINE_LINUX_DEFAULT` |
| [`user-manage`](/user-manage/)   | `instance.{crypted_root_password, ssh_keys, users[], sshd{}}` | `chpasswd` + `sed PermitRootLogin` cexec |
| [`grub2disk` (UEFI mode)](/grub2disk/) | env only (`MODE=efi`, `EFI_PARTITION`, `BOOTLOADER_ID`) | Two nearly-identical 5-line `grub-install` cexec blobs per template |

All four actions chroot into a root filesystem mounted from
`$BLOCK_DEVICE` (the root LV or md array of the target host) and apply
their config idempotently. If the relevant metadata block is absent,
they no-op rather than fail — safe to leave a `serial-console` step in
a template even for hosts without a serial console.

See the per-action READMEs for metadata schemas and example workflow
steps, or [`docs/plans/2026-04-23-metadata-aware-actions-design.md`](/docs/plans/2026-04-23-metadata-aware-actions-design.md)
for the cross-cutting design.

### Tinkerbell side

The new `users[]`, `sshd{}`, and `console{}` metadata fields require a
corresponding passthrough in tootles' `HackInstance` JSON so the
actions can see them. Fork of tinkerbell/tinkerbell with those fields
applied lives at `ghcr.io/jasonyates/tinkerbell:latest`; the canonical
branch is `main` on [the fork](https://github.com/jasonyates/tinkerbell).

## Releases

Actions are released on a per revision basis. With each PR merged, all Actions are built and pushed
to quay.io tagged with the Git revision. The `latest` tag is updated to point to the new image.

We try not to make changes that would break Actions, but we do not provide a backward compatibility
guarantee. We recommend using the static Git revision tag for most deployments.

Our release process may provide stronger compatibility guarantees in the future.

**This fork** publishes every action in the matrix to
`ghcr.io/jasonyates/tinkerbell-actions/<name>:{latest,<sha>}` on push
to `main` via [`.github/workflows/release-ghcr.yml`](/.github/workflows/release-ghcr.yml).
When adding a new action directory with a `Dockerfile`, add its name
to the matrix in that workflow so it ships.

## Community Actions

[Actions](https://tinkerbell.org/docs/concepts/templates/#action) are one of the best parts of Tinkerbell. These reusable building blocks allow us to evolve the way we provision and interact with machines. And sharing Actions is a great way to participate in this evolution. The Actions below are built and maintained by community members, like you! To add your own Action to the list, raise a PR. If you find an Action that's no longer maintained, please raise an issue or PR to have it removed.

A couple recommendations for making your Action as community friendly as possible:

- Host your Action in a container registry that's publicly accessible. Here's an [example Github Action](docs/example-publish.yaml) that builds and pushes an image to `ghcr.io`.
- Include a README with usage instructions and examples.

### Actions List

- [waitdaemon](https://github.com/jacobweinstock/waitdaemon) - Run an Action that always reports successful. Useful for reboot, poweroff, or kexec Actions.
