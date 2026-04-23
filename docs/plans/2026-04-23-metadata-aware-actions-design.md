# Metadata-aware Tinkerbell actions — design

Date: 2026-04-23
Status: Agreed, ready to plan/implement

## Goal

Replace the hand-rolled `cexec` one-liners that currently do OS
configuration during provisioning with a small set of purpose-built
actions that fetch metadata from Tinkerbell and apply it inside the
target chroot. Remove drift between metadata and applied config, make
templates readable, and stop accumulating bash heredocs.

First wave, scoped to what the current templates need most:

1. `serial-console` — set the kernel/grub console to a specific tty+baud
2. `user-manage` — create users, set passwords, install SSH keys, tune sshd
3. `rootio-fstab` — generate `/etc/fstab` from `storage.filesystems`
   metadata (new subcommand on the existing rootio binary)
4. `grub2disk` (modernised in place) — add UEFI support, NVRAM
   registration, and firmware fallback path so the hand-rolled
   grub-install cexec blocks can retire

All four read metadata from the same Hegel endpoint rootio already uses
(`http://$MIRROR_HOST:$METADATA_SERVICE_PORT/metadata`).

## Architectural decisions

- **Option A metadata fetching.** Every action fetches
  `/metadata` itself and pulls the fields it needs from a typed Go
  struct. No template-side hardwareMap wiring beyond the env vars
  already used by rootio. Chosen over env-only input because the
  platform is already metadata-driven (rootio + tootles
  passthrough) and adding a second source of truth would drift.

- **Shared `pkg/metadata` package.** One Go package with `Client`,
  `Fetch()`, and the full `Metadata` struct (matches tootles'
  `HackInstance` JSON shape verbatim). Rootio's existing
  `storage/metadata.go` is retired and re-aliased from the shared
  package so callers don't break.

- **Shared `pkg/chroot` helper.** Generalise the one-off chroot
  setup currently baked into `grub2disk/grub/grub.go`. Actions call
  `chroot.Enter(blockDev, fsType)` once and the rest of `main`
  executes inside the chroot. No teardown — container exit is the
  teardown.

- **Extend grub2disk, don't fork.** `grub2disk` exists today as a
  BIOS-only wrapper. Teach it a `MODE=efi|bios` env var plus the EFI
  partition path, and it replaces both `grub-install-sda` and
  `grub-install-sdb` cexec blocks in the templates. One action, two
  modes.

- **rootio-fstab as subcommand.** No new image; extend the rootio
  binary with a `fstab` subcommand alongside the existing
  `partition`, `format`, `mount`, `wipe`, `version`. Shares the
  metadata client, container, and release pipeline.

## Tinkerbell metadata additions

Three new types on `MetadataInstance` in
`api/v1alpha1/tinkerbell/hardware.go`. Mirrors the pattern used for
RAID and LVM passthrough: add types, regenerate deepcopy + CRDs,
extend `HackInstance` inline struct in `pkg/data/instance.go`, add
a passthrough test.

### 1. `Console`

```go
type MetadataInstance struct {
    // ... existing fields ...
    Console *MetadataInstanceConsole `json:"console,omitempty"`
}

type MetadataInstanceConsole struct {
    TTY  string `json:"tty,omitempty"`   // e.g. "ttyS1"
    Baud int    `json:"baud,omitempty"`  // e.g. 115200
}
```

Action contract: `Console == nil || TTY == ""` → no-op. Otherwise,
rewrite `/etc/default/grub.d/50-cloudimg-settings.cfg` (Ubuntu) or
`/etc/default/grub` (generic), regenerate `grub.cfg`.

### 2. `Users` + back-compat with root-specific fields

```go
type MetadataInstance struct {
    // back-compat fields stay put; both apply to root when set
    CryptedRootPassword string   `json:"crypted_root_password,omitempty"`
    SSHKeys             []string `json:"ssh_keys,omitempty"`

    // new: additional (non-root) users
    Users []*MetadataInstanceUser `json:"users,omitempty"`

    // new: global sshd config
    SSHD *MetadataInstanceSSHD `json:"sshd,omitempty"`
}

type MetadataInstanceUser struct {
    Username          string   `json:"username"`
    CryptedPassword   string   `json:"crypted_password,omitempty"`
    SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty"`
    Sudo              bool     `json:"sudo,omitempty"`
    Shell             string   `json:"shell,omitempty"` // default /bin/bash
}
```

### 3. `SSHD` — global sshd_config

```go
type MetadataInstanceSSHD struct {
    PermitRootLogin        string `json:"permit_root_login,omitempty"`
    // "yes" | "prohibit-password" | "no" | "forced-commands-only"
    PasswordAuthentication *bool  `json:"password_authentication,omitempty"`
    // pointer so nil means "leave sshd_config default untouched"
}
```

Separation of concerns: `Users[]` answers "who exists on the
system"; `SSHD` answers "how does sshd behave". They're orthogonal
so the operator doesn't have to model one to control the other.

## Shared `pkg/metadata` package

```
actions/pkg/metadata/
  metadata.go       # Client + types
  metadata_test.go  # JSON round-trip + stub HTTP server
```

```go
package metadata

type Client struct {
    baseURL string
    http    *http.Client
}

// New reads MIRROR_HOST (required) and METADATA_SERVICE_PORT
// (default 50061) from env and returns a Client.
func New() (*Client, error)

// Fetch retrieves /metadata and unmarshals into Metadata.
func (c *Client) Fetch(ctx context.Context) (*Metadata, error)

type Metadata struct {
    Instance Instance `json:"instance"`
    Facility Facility `json:"facility,omitempty"`
}

type Instance struct {
    Hostname            string    `json:"hostname,omitempty"`
    CryptedRootPassword string    `json:"crypted_root_password,omitempty"`
    SSHKeys             []string  `json:"ssh_keys,omitempty"`
    Users               []User    `json:"users,omitempty"`
    SSHD                *SSHD     `json:"sshd,omitempty"`
    Console             *Console  `json:"console,omitempty"`
    Storage             Storage   `json:"storage,omitempty"`
    OS                  OperatingSystem `json:"operating_system_version,omitempty"`
}

// ... User, SSHD, Console, Storage (with Disk, RAID, VolumeGroup,
//     LogicalVolume, Filesystem), Facility, OperatingSystem ...
```

Field names and JSON tags match tootles' `HackInstance` exactly so
the wire format is one source of truth.

### Migration of rootio

Delete `rootio/storage/metadata.go`. Re-route
`storage.RetrieveData()` to call `metadata.New().Fetch()` and
return a `*metadata.Metadata`. Introduce type aliases in
`rootio/storage` (`type RAID = metadata.RAID`, etc.) so existing
callers keep compiling.

## Shared `pkg/chroot` helper

```go
package chroot

// Enter mounts blockDev (fsType) at /mountAction, bind-mounts
// /dev, /proc, /sys inside it, and chroots there. No teardown —
// action containers are short-lived so the kernel reclaims mounts
// on exit. Subsequent exec.Command sees the new root.
func Enter(blockDev, fsType string) error
```

Generalised from `grub2disk/grub/grub.go`'s existing mount+chroot
logic. ~40 lines.

## Per-action designs

### serial-console

- Reads `Instance.Console.{TTY, Baud}`. No-op if absent.
- `chroot.Enter(blockDev, fsType)`.
- Rewrites `console=` kernel cmdline in
  `/etc/default/grub.d/50-cloudimg-settings.cfg` (Ubuntu; falls
  back to `/etc/default/grub`).
- Runs `grub-mkconfig -o /boot/grub/grub.cfg` and `chmod 644` it.

Env: `BLOCK_DEVICE`, `FS_TYPE` (same as every other chroot action).

### user-manage

- Reads `Instance.CryptedRootPassword`, `Instance.SSHKeys`,
  `Instance.Users`, `Instance.SSHD`.
- `chroot.Enter(blockDev, fsType)`.
- Apply order:
  1. Root password via `chpasswd -e` if `CryptedRootPassword` set.
  2. Root `authorized_keys` at `/root/.ssh/authorized_keys`
     (mode 0600, owner 0:0) if `SSHKeys` set.
  3. For each `User`:
     - `useradd -m -s <shell>` if not present.
     - `chpasswd -e` if `CryptedPassword` set.
     - `authorized_keys` at `/home/<user>/.ssh/authorized_keys`
       (mode 0600, owner user:user).
     - Add to `sudo` group if `Sudo: true`.
  4. If `SSHD.PermitRootLogin` non-empty → replace directive in
     `/etc/ssh/sshd_config`.
  5. If `SSHD.PasswordAuthentication` non-nil → replace directive
     (value "yes" or "no").
- Never restart sshd — the OS hasn't booted yet. Directives take
  effect on first boot.

### rootio-fstab (subcommand)

- New Cobra command `rootio fstab` in `rootio/cmd/rootio.go`.
- Reads `Instance.Storage.Filesystems`, extracts label from each
  filesystem's `create.options` list (looks for `-L` or `-n`,
  depending on format).
- `chroot.Enter(blockDev, fsType)`.
- Writes `/etc/fstab` with one line per filesystem:
  ```
  LABEL=<label>  <point>  <format>  <opts>  <dump>  <pass>
  ```
- Default mount opts by format: `ext4` → `defaults`, `vfat` →
  `defaults,nofail`, `xfs` → `defaults`. Dump 0, pass 1 for `/`, 2
  for others.
- Deterministic, idempotent. Replaces the multi-line printf in
  every template.

### grub2disk (modernised)

Retain current env contract and add:
- `MODE=bios|efi` (default bios for back-compat)
- `EFI_PARTITION=/dev/sda1` (required when `MODE=efi`)
- `BOOTLOADER_ID=ubuntu` (default; used for `--bootloader-id` and
  the `/EFI/<id>/` directory name)

In `MODE=efi`:
1. `chroot.Enter(blockDev, fsType)`.
2. `mount <EFI_PARTITION> /boot/efi`.
3. `grub-install --target=x86_64-efi --efi-directory=/boot/efi
   --bootloader-id=<id> --recheck --no-nvram`.
4. `update-grub`.
5. Copy fallback path:
   `/boot/efi/EFI/<id>/shimx64.efi` → `/boot/efi/EFI/BOOT/BOOTX64.EFI`
   (or `grubx64.efi` if shim absent).
6. `umount /boot/efi`.
7. Best-effort `efibootmgr --create --disk <disk> --part <N>
   --label <id> --loader '\EFI\<id>\shimx64.efi'`.

In `MODE=bios`: existing behaviour (`grub-install <disk>`), no
changes.

Templates call it twice (once per disk) with distinct
`BOOTLOADER_ID` (`ubuntu` / `ubuntu-backup`) to preserve the
mirror-safe boot story.

## Repo layout

```
actions/
  pkg/
    metadata/                  # NEW: shared HTTP + struct defs
    chroot/                    # NEW: mount+chroot helper
  serial-console/              # NEW action
    cmd/main.go
    internal/serial/           # pure-Go config rewriter
    Dockerfile
    README.md
  user-manage/                 # NEW action
    cmd/main.go
    internal/users/
    Dockerfile
    README.md
  rootio/
    cmd/rootio.go              # MODIFIED: add `fstab` subcommand
    storage/metadata.go        # DELETED (re-aliased from pkg/metadata)
    fstab/                     # NEW: fstab rendering
    fstab/fstab_test.go
  grub2disk/
    main.go                    # MODIFIED: MODE + EFI_PARTITION
    grub/                      # MODIFIED: extract InstallEFI/InstallBIOS
```

## Testing

Each action splits into:
- A thin `cmd/main.go` that parses env, calls metadata, calls
  chroot, and calls the config-rewrite package. Not unit-tested.
- An `internal/<domain>/` package with pure functions (no chroot,
  no exec) that take typed input and return text/file-intent.
  Golden-file unit tests for every realistic metadata shape.

`pkg/metadata` gets:
- `TestFetch` against an `httptest.Server` that serves a fixture.
- `TestUnmarshal` against `rootio/test/lvm.json` (already rich).

`pkg/chroot` is tested end-to-end on a Linux host (or in CI on a
Linux runner). Not unit-testable from macOS.

## Tinkerbell side — summary of work

Mirrors the RAID/LVM passthrough pattern already landed:

1. Add `MetadataInstanceConsole`, `MetadataInstanceUser`,
   `MetadataInstanceSSHD` types to
   `api/v1alpha1/tinkerbell/hardware.go`.
2. Wire `Console`, `Users`, `SSHD` fields into `MetadataInstance`.
3. Regenerate deepcopy + CRDs (`make generate && make manifests`).
4. Extend `HackInstance` inline struct in `pkg/data/instance.go`
   with matching fields/tags.
5. Add passthrough tests to
   `tootles/internal/backend/backend_test.go`.

No EC2 subtree changes — these fields are consumed by actions via
`/metadata`, not the EC2 service tree.

## Out of scope (future waves)

- `netplan-render` — render netplan YAML from
  `metadata.instance.network`. The network subtree landed recently
  in tootles but isn't yet passed through HackInstance. One
  iteration away.
- `cloud-init-datasource` — still covered by `writefile` + the
  current tootles URL. Easy to fold in later.
- `hostname` — two-line cexec; not worth its own action until we
  see repeat pain.
- netplan / bond / VLAN rendering inside `user-manage` or
  elsewhere.

## Implementation ordering (provisional)

1. **`pkg/metadata`** (foundational; rootio refactors onto it).
2. **`pkg/chroot`** (used by all new actions + refactored
   grub2disk).
3. **Tinkerbell metadata additions** (Console, Users, SSHD
   passthrough) — can land in parallel.
4. **`rootio fstab` subcommand** (smallest action; tests the
   shared packages end-to-end).
5. **`serial-console` action**.
6. **`user-manage` action**.
7. **`grub2disk` UEFI mode**.
8. **Update `ubuntu-noble-raid1-efi.yaml` and
   `ubuntu-noble-lvm-raid1-efi.yaml`** to replace the cexec blocks
   with the new actions.
