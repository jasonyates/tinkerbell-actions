# serial-console

Configure the serial console of an installed OS during provisioning.
Reads `metadata.instance.console.{tty, baud}` from Hegel, rewrites
`/etc/default/grub.d/50-cloudimg-settings.cfg` inside the target
root, and runs `grub-mkconfig -o /boot/grub/grub.cfg`.

If no `console` block is present in metadata, the action is a no-op
(useful when the same template applies to hosts with and without
serial requirements).

## Environment variables

| Var | Required | Notes |
|---|---|---|
| `BLOCK_DEVICE` | yes | Absolute path to the root block device, e.g. `/dev/vg0/root` |
| `FS_TYPE` | yes | Filesystem of the root device, e.g. `ext4` |
| `MIRROR_HOST` | yes | Hegel host |
| `METADATA_SERVICE_PORT` | no | Hegel port, default `50061` |

## Metadata schema

```yaml
metadata:
  instance:
    console:
      tty: ttyS1
      baud: 115200
```

`baud: 0` or omitted defaults to 115200.

## Example workflow step

```yaml
- name: "serial-console"
  image: ghcr.io/jasonyates/tinkerbell-actions/serial-console:latest
  timeout: 60
  environment:
    BLOCK_DEVICE: "/dev/vg0/root"
    FS_TYPE: "ext4"
    METADATA_SERVICE_PORT: "7080"
    MIRROR_HOST: "31.24.228.5"
```
