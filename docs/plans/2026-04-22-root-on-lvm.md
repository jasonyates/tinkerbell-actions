# Root-on-LVM Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship root-on-LVM-on-RAID1 provisioning across tinkerbell (CR + Hegel passthrough), actions/rootio (driver fix + validation + preflight), actions/scripts (rootfs tarball), and actions/docs/templates (new template).

**Architecture:** See `docs/plans/2026-04-22-root-on-lvm-design.md`. Landing order: tinkerbell passthrough → rootio driver work → rootfs tarball → new template.

**Tech Stack:** Go 1.21+ (rootio, tinkerbell), mdadm 4.1 + lvm2-static (rootio Dockerfile), Ubuntu Noble rootfs (debootstrap), Tinkerbell workflows (Hardware CR, Hegel, tink-agent), shell (bash -eux) for templates.

**Repos:**
- **actions:** `/Users/jason.yates79/Documents/GitHub/actions` (branch `feat/root-on-lvm`, already created)
- **tinkerbell:** `/Users/jason.yates79/Documents/GitHub/tinkerbell` (currently on `feat/metadata-network` — needs a new branch for this work)

---

## Task 1: tinkerbell — new feature branch

**Files:** none (git branch op)

**Step 1:** From tinkerbell repo, create branch off `main`:
```bash
cd /Users/jason.yates79/Documents/GitHub/tinkerbell
git fetch origin
git checkout -b feat/metadata-lvm-passthrough origin/main
```

**Step 2:** Verify:
```bash
git branch --show-current   # feat/metadata-lvm-passthrough
git log --oneline -3        # should match origin/main
```

No commit — just branch setup.

---

## Task 2: tinkerbell — failing passthrough test

**Files:**
- Modify: `pkg/backend/kube/tootles_test.go` (append new test)

**Step 1:** Read `pkg/backend/kube/tootles_test.go` to find the existing `TestToHackInstance_PassesThroughRAID` test for style reference.

**Step 2:** Append a new test `TestToHackInstance_PassesThroughVolumeGroups`. Mirrors the RAID test but asserts `storage.volume_groups` round-trips through `toHackInstance`:

```go
func TestToHackInstance_PassesThroughVolumeGroups(t *testing.T) {
	hw := v1alpha1.Hardware{
		Spec: v1alpha1.HardwareSpec{
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Storage: &v1alpha1.MetadataInstanceStorage{
						VolumeGroups: []*v1alpha1.MetadataInstanceStorageVolumeGroup{
							{
								Name:            "vg0",
								PhysicalVolumes: []string{"/dev/md0"},
								LogicalVolumes: []*v1alpha1.MetadataInstanceStorageLogicalVolume{
									{Name: "root", Size: 42949672960},
									{Name: "docker", Size: 0},
								},
							},
						},
					},
				},
			},
		},
	}

	hi, err := toHackInstance(hw)
	if err != nil {
		t.Fatalf("toHackInstance: %v", err)
	}

	out, err := json.Marshal(hi)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed struct {
		Metadata struct {
			Instance struct {
				Storage struct {
					VolumeGroups []struct {
						Name            string   `json:"name"`
						PhysicalVolumes []string `json:"physical_volumes"`
						LogicalVolumes  []struct {
							Name string `json:"name"`
							Size uint64 `json:"size"`
						} `json:"logical_volumes"`
					} `json:"volume_groups"`
				} `json:"storage"`
			} `json:"instance"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}

	vgs := parsed.Metadata.Instance.Storage.VolumeGroups
	if len(vgs) != 1 || vgs[0].Name != "vg0" {
		t.Fatalf("vgs = %+v; want 1 entry named vg0\nJSON=%s", vgs, out)
	}
	if len(vgs[0].PhysicalVolumes) != 1 || vgs[0].PhysicalVolumes[0] != "/dev/md0" {
		t.Errorf("vgs[0].PhysicalVolumes = %v, want [/dev/md0]", vgs[0].PhysicalVolumes)
	}
	if len(vgs[0].LogicalVolumes) != 2 {
		t.Fatalf("vgs[0].LogicalVolumes = %+v; want 2 entries", vgs[0].LogicalVolumes)
	}
	if vgs[0].LogicalVolumes[0].Name != "root" || vgs[0].LogicalVolumes[0].Size != 42949672960 {
		t.Errorf("lv[0] = %+v, want {root, 42949672960}", vgs[0].LogicalVolumes[0])
	}
	if vgs[0].LogicalVolumes[1].Name != "docker" || vgs[0].LogicalVolumes[1].Size != 0 {
		t.Errorf("lv[1] = %+v, want {docker, 0}", vgs[0].LogicalVolumes[1])
	}
}
```

**Step 3:** Run — expected **COMPILE FAILURE** (types don't exist yet):
```bash
go test ./pkg/backend/kube/... -run TestToHackInstance_PassesThroughVolumeGroups
```
Expected error mentions `MetadataInstanceStorageVolumeGroup` / `MetadataInstanceStorageLogicalVolume` / `VolumeGroups` undefined.

**Step 4:** Do not commit yet — Tasks 3–5 make this test pass, then commit together.

---

## Task 3: tinkerbell — add CR types

**Files:**
- Modify: `api/v1alpha1/tinkerbell/hardware.go`

**Step 1:** In `hardware.go`, locate `MetadataInstanceStorage` (line ~289) and add a `VolumeGroups` field:

```go
type MetadataInstanceStorage struct {
	Disks        []*MetadataInstanceStorageDisk        `json:"disks,omitempty"`
	Raid         []*MetadataInstanceStorageRAID        `json:"raid,omitempty"`
	VolumeGroups []*MetadataInstanceStorageVolumeGroup `json:"volume_groups,omitempty"`
	Filesystems  []*MetadataInstanceStorageFilesystem  `json:"filesystems,omitempty"`
}
```
(Preserve existing fields — the diff is a single new line plus the slot.)

**Step 2:** Add two new types in the same file, adjacent to `MetadataInstanceStorageRAID`:

```go
// MetadataInstanceStorageVolumeGroup describes a single LVM volume group.
// Field names and JSON tags match rootio's storage.VolumeGroup type exactly.
type MetadataInstanceStorageVolumeGroup struct {
	Name            string                                  `json:"name,omitempty"`
	PhysicalVolumes []string                                `json:"physical_volumes,omitempty"`
	LogicalVolumes  []*MetadataInstanceStorageLogicalVolume `json:"logical_volumes,omitempty"`
	Tags            []string                                `json:"tags,omitempty"`
}

// MetadataInstanceStorageLogicalVolume describes a single LV inside a VG.
// Size is in bytes; 0 means "use 100%FREE" (only valid on the last LV in a VG).
type MetadataInstanceStorageLogicalVolume struct {
	Name string   `json:"name,omitempty"`
	Size uint64   `json:"size,omitempty"`
	Tags []string `json:"tags,omitempty"`
	Opts []string `json:"opts,omitempty"`
}
```

**Step 3:** Build — still expected to fail (deepcopy missing):
```bash
go build ./...
```
Expected error: `missing method DeepCopyInto` on the new types.

---

## Task 4: tinkerbell — regenerate deepcopy + CRDs

**Files:**
- Modify (generated): `api/v1alpha1/tinkerbell/zz_generated.deepcopy.go`
- Modify (generated): any file under `crd/` that holds the Hardware CRD schema

**Step 1:** Run:
```bash
make generate
```
If this fails because `controller-gen` isn't installed, install via the Makefile target:
```bash
make $(grep CONTROLLER_GEN_FQP Makefile | head -1 | awk -F':=' '{print $2}' | xargs)
```
(Or read the Makefile and find the right bootstrap — see lines 55, 185–186, 264–265.)

**Step 2:** Also run:
```bash
make manifests   # regenerates CRD YAML under crd/
```
(If no such target, check `make generate-all`.)

**Step 3:** Verify:
```bash
git diff --stat
```
Expected: `zz_generated.deepcopy.go` has new `DeepCopyInto` methods for `MetadataInstanceStorageVolumeGroup` and `MetadataInstanceStorageLogicalVolume`. CRD YAML under `crd/` has new `volumeGroups` entries.

**Step 4:** Build:
```bash
go build ./...
```
Expected: PASS.

---

## Task 5: tinkerbell — extend HackInstance + pass test

**Files:**
- Modify: `pkg/data/instance.go`

**Step 1:** In `pkg/data/instance.go`, find the `HackInstance` struct and locate the inline `Storage` block (the same one the RAID commit added `Raid` to). Add a `VolumeGroups` inline slice adjacent to `Raid`:

```go
VolumeGroups []struct {
	Name            string   `json:"name,omitempty"`
	PhysicalVolumes []string `json:"physical_volumes,omitempty"`
	LogicalVolumes  []struct {
		Name string   `json:"name,omitempty"`
		Size uint64   `json:"size,omitempty"`
		Tags []string `json:"tags,omitempty"`
		Opts []string `json:"opts,omitempty"`
	} `json:"logical_volumes,omitempty"`
	Tags []string `json:"tags,omitempty"`
} `json:"volume_groups,omitempty"`
```

**Step 2:** Find `toHackInstance` (in `pkg/backend/kube/tootles.go`). Whatever it does for `Raid`, mirror for `VolumeGroups` — the RAID field was handled by the same JSON round-trip mechanism, so if the JSON tags align the addition is automatic. If the RAID passthrough does an explicit copy loop, add the equivalent copy loop for VGs. Read the RAID handling in `toHackInstance` before deciding.

**Step 3:** Run the test:
```bash
go test ./pkg/backend/kube/... -run TestToHackInstance_PassesThroughVolumeGroups -v
```
Expected: PASS.

**Step 4:** Run full test package to make sure nothing else broke:
```bash
go test ./pkg/backend/kube/...
```
Expected: PASS.

**Step 5:** Commit:
```bash
git add api/v1alpha1/tinkerbell/hardware.go \
        api/v1alpha1/tinkerbell/zz_generated.deepcopy.go \
        crd/ \
        pkg/data/instance.go \
        pkg/backend/kube/tootles.go \
        pkg/backend/kube/tootles_test.go
git commit -m "$(cat <<'EOF'
tootles: pass through storage.volume_groups in HackInstance

Mirrors the RAID passthrough work (commit 82e7fcab) but larger: the
Hardware CR had no LVM types before, so adds MetadataInstanceStorage-
VolumeGroup and MetadataInstanceStorageLogicalVolume alongside the
existing RAID type, wires a VolumeGroups field into
MetadataInstanceStorage, regenerates deepcopy + CRDs, and extends the
HackInstance JSON shape so rootio sees storage.volume_groups when it
hits /metadata.

Field names and JSON tags match rootio's storage.VolumeGroup /
storage.LogicalVolume types exactly, so no translation layer is
needed on the rootio side.

Test asserts a Hardware with storage.volume_groups populated round-
trips through toHackInstance with the VG + LV list intact.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: tinkerbell — add branch to release workflow

**Files:**
- Modify: `.github/workflows/release-ghcr-fork.yaml`

**Step 1:** Add `feat/metadata-lvm-passthrough` to the `on.push.branches` list alongside the existing RAID branch:

```yaml
on:
  push:
    branches:
      - main
      - feat/metadata-raid-passthrough
      - feat/metadata-lvm-passthrough
  workflow_dispatch: {}
```

**Step 2:** Commit:
```bash
git add .github/workflows/release-ghcr-fork.yaml
git commit -m "$(cat <<'EOF'
ci: publish fork image for feat/metadata-lvm-passthrough

Add the LVM passthrough branch to the GHCR fork workflow trigger list
so the tinkerbell image auto-publishes on push.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

**Step 3:** Push:
```bash
git push -u origin feat/metadata-lvm-passthrough
```
Wait for the workflow to publish `ghcr.io/<owner>/tinkerbell:<sha>` before moving on — the template rollout at the end needs this image tag.

---

## Task 7: rootio — failing test for volume_groups unmarshalling

**Files:**
- Create: `rootio/storage/lvm_test.go`

**Location:** `/Users/jason.yates79/Documents/GitHub/actions` (branch `feat/root-on-lvm` already checked out)

**Step 1:** Create `rootio/storage/lvm_test.go` with a metadata unmarshal test mirroring `TestMetadataUnmarshalsRAID` in `raid_test.go`:

```go
package storage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMetadataUnmarshalsVolumeGroups(t *testing.T) {
	body := []byte(`{
	  "metadata": {
	    "instance": {
	      "storage": {
	        "volume_groups": [
	          {
	            "name": "vg0",
	            "physical_volumes": ["/dev/md0"],
	            "logical_volumes": [
	              {"name": "root", "size": 42949672960},
	              {"name": "docker", "size": 0}
	            ],
	            "tags": ["provisioned"]
	          }
	        ]
	      }
	    }
	  }
	}`)

	var w Wrapper
	if err := json.Unmarshal(body, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := w.Metadata.Instance.Storage.VolumeGroups
	if len(got) != 1 {
		t.Fatalf("want 1 vg, got %d", len(got))
	}
	want := VolumeGroup{
		Name:            "vg0",
		PhysicalVolumes: []string{"/dev/md0"},
		LogicalVolumes: []LogicalVolume{
			{Name: "root", Size: 42949672960},
			{Name: "docker", Size: 0},
		},
		Tags: []string{"provisioned"},
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %+v\nwant %+v", got[0], want)
	}
}
```

**Step 2:** Run:
```bash
cd /Users/jason.yates79/Documents/GitHub/actions/rootio
go test ./storage/ -run TestMetadataUnmarshalsVolumeGroups -v
```
Expected: PASS (the metadata struct already has `VolumeGroups`).

**Step 3:** Commit:
```bash
cd /Users/jason.yates79/Documents/GitHub/actions
git add rootio/storage/lvm_test.go
git commit -m "$(cat <<'EOF'
rootio: cover metadata unmarshalling for storage.volume_groups

The VolumeGroup and LogicalVolume types have been in rootio's metadata
struct since initial import but were never exercised by a test. Add a
round-trip unmarshal test as a baseline before adding LVM validation
and driver changes.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: rootio — ValidateVolumeGroup with failing tests first

**Files:**
- Modify: `rootio/storage/lvm_test.go` (append)
- Modify: `rootio/storage/lvm.go` (add ValidateVolumeGroup)

**Step 1:** In `rootio/storage/lvm_test.go`, append a table-driven test mirroring `TestValidateRAID`:

```go
func TestValidateVolumeGroup(t *testing.T) {
	baseLV := LogicalVolume{Name: "root", Size: 1 << 30}
	fillLV := LogicalVolume{Name: "docker", Size: 0}

	cases := []struct {
		name    string
		vg      VolumeGroup
		wantErr string
	}{
		{"empty name", VolumeGroup{PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{baseLV}}, "name"},
		{"bad vg name chars", VolumeGroup{Name: "bad name", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{baseLV}}, "name"},
		{"no physical volumes", VolumeGroup{Name: "vg0", LogicalVolumes: []LogicalVolume{baseLV}}, "physical"},
		{"non-absolute PV", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"md0"}, LogicalVolumes: []LogicalVolume{baseLV}}, "absolute"},
		{"bad lv name", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{{Name: "bad name", Size: 1}}}, "name"},
		{"two fill LVs", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{fillLV, fillLV}}, "size=0"},
		{"fill LV not last", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{fillLV, baseLV}}, "last"},
		{"valid simple", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{baseLV}}, ""},
		{"valid with fill last", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{baseLV, fillLV}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVolumeGroup(tc.vg)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
```

**Step 2:** Run — expected FAIL (function doesn't exist):
```bash
cd /Users/jason.yates79/Documents/GitHub/actions/rootio
go test ./storage/ -run TestValidateVolumeGroup -v
```

**Step 3:** Implement `ValidateVolumeGroup` in `rootio/storage/lvm.go`. Imports, then append:

```go
import (
	"fmt"
	"strings"

	"github.com/tinkerbell/actions/rootio/lvm"
)

// ValidateVolumeGroup checks a VolumeGroup against basic requirements:
//   - VG name valid
//   - at least one PV, each an absolute device path
//   - LV names valid, at most one LV with Size==0 and it must be last
//   - tags valid
func ValidateVolumeGroup(vg VolumeGroup) error {
	if err := lvm.ValidateVolumeGroupName(vg.Name); err != nil {
		return err
	}
	if len(vg.PhysicalVolumes) == 0 {
		return fmt.Errorf("lvm: volume group %q has no physical volumes", vg.Name)
	}
	for _, pv := range vg.PhysicalVolumes {
		if !strings.HasPrefix(pv, "/") {
			return fmt.Errorf("lvm: physical volume %q must be an absolute device path", pv)
		}
	}
	for _, tag := range vg.Tags {
		if err := lvm.ValidateTag(tag); err != nil {
			return err
		}
	}
	for i, lv := range vg.LogicalVolumes {
		if err := lvm.ValidateLogicalVolumeName(lv.Name); err != nil {
			return err
		}
		for _, tag := range lv.Tags {
			if err := lvm.ValidateTag(tag); err != nil {
				return err
			}
		}
		if lv.Size == 0 && i != len(vg.LogicalVolumes)-1 {
			return fmt.Errorf("lvm: logical volume %q has size=0 but is not last in volume group %q", lv.Name, vg.Name)
		}
	}
	// Second pass: at most one size=0 LV (already implicit from the "must be last" rule,
	// but double-check in case of an empty VG).
	fills := 0
	for _, lv := range vg.LogicalVolumes {
		if lv.Size == 0 {
			fills++
		}
	}
	if fills > 1 {
		return fmt.Errorf("lvm: volume group %q has multiple logical volumes with size=0; only one (the last) may fill remaining space", vg.Name)
	}
	return nil
}
```

**Step 4:** Wire into existing `CreateVolumeGroup` by adding the validate call as the first line of the function body in `rootio/storage/lvm.go`:

```go
func CreateVolumeGroup(volumeGroup VolumeGroup) error {
	if err := ValidateVolumeGroup(volumeGroup); err != nil {
		return err
	}
	// ... existing body ...
}
```

**Step 5:** Run — expected PASS:
```bash
go test ./storage/ -v
```

**Step 6:** Commit:
```bash
git add rootio/storage/lvm.go rootio/storage/lvm_test.go
git commit -m "$(cat <<'EOF'
rootio: validate VolumeGroup before creating

Mirror the ValidateRAID pattern: name regex via lvm.ValidateVolumeGroupName,
non-empty PV list with absolute paths, LV name validation via
lvm.ValidateLogicalVolumeName, tag validation, and the invariant that
at most one LV per VG may have size=0 (maps to -l 100%FREE) and it
must be last.

CreateVolumeGroup now runs validation before any lvm2 commands, so
misconfigured metadata fails fast with a clear error instead of
partially applying.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: rootio — fix `/sbin/lvm` path bug

**Files:**
- Modify: `rootio/lvm/lvm.go`

**Step 1:** In `rootio/lvm/lvm.go`, replace every `run("lvm", ...)` with `run("/sbin/lvm", ...)`. Locations (approx line numbers from current state): 25, 38 (via PVScan), 50 (via VGScan), 100 (CreateVolumeGroup), 160 (CreateLogicalVolume).

Simplest: `sed`-style review but do via Edit tool for correctness. One call per edit.

**Step 2:** Build:
```bash
cd /Users/jason.yates79/Documents/GitHub/actions/rootio
go build ./...
```
Expected: PASS.

**Step 3:** Run existing tests to catch any accidental breakage:
```bash
go test ./...
```
Expected: PASS.

**Step 4:** Commit:
```bash
git add rootio/lvm/lvm.go
git commit -m "$(cat <<'EOF'
rootio: call /sbin/lvm by absolute path

The lvm driver called run("lvm", ...) but the rootio image is built
FROM scratch with lvm.static at /sbin/lvm and no PATH, so
exec.Command("lvm") would fail with "executable file not found in
$PATH". Matches the /sbin/mdadm pattern in raid.go.

This is why the LVM metadata path, though already wired into the
partition command, has never actually executed successfully.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: rootio — TeardownVolumeGroups preflight

**Files:**
- Modify: `rootio/storage/lvm.go` (add TeardownVolumeGroups)
- Modify: `rootio/storage/lvm_test.go` (optional unit test for arg construction)
- Modify: `rootio/lvm/lvm.go` (add helper wrappers if missing: DeactivateVolumeGroup, RemoveVolumeGroup, RemovePhysicalVolume)

**Step 1:** In `rootio/lvm/lvm.go`, add wrappers for `vgchange -an`, `vgremove -ff`, `pvremove -ff`. Best-effort (log, ignore non-zero exit):

```go
// DeactivateVolumeGroup deactivates a VG if present. Non-fatal if missing.
func DeactivateVolumeGroup(name string) error {
	c := exec.Command("/sbin/lvm", "vgchange", "-an", name)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	_ = c.Run() // best-effort
	return nil
}

// RemoveVolumeGroup removes a VG + all its LVs. Non-fatal if missing.
func RemoveVolumeGroup(name string) error {
	c := exec.Command("/sbin/lvm", "vgremove", "-ff", name)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	_ = c.Run()
	return nil
}

// RemovePhysicalVolume clears LVM PV signature from dev. Non-fatal.
func RemovePhysicalVolume(dev string) error {
	c := exec.Command("/sbin/lvm", "pvremove", "-ff", "-y", dev)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	_ = c.Run()
	return nil
}
```

**Step 2:** In `rootio/storage/lvm.go`, add:

```go
// TeardownVolumeGroups deactivates and removes each VG in metadata, then
// clears PV signatures from every PV. Best-effort; mirrors the RAID
// preflight in rootio cmd's wipe/partition paths. Call BEFORE StopRAID.
func TeardownVolumeGroups(vgs []VolumeGroup) {
	for _, vg := range vgs {
		_ = lvm.DeactivateVolumeGroup(vg.Name)
		_ = lvm.RemoveVolumeGroup(vg.Name)
	}
	for _, vg := range vgs {
		for _, pv := range vg.PhysicalVolumes {
			_ = lvm.RemovePhysicalVolume(pv)
		}
	}
}
```

**Step 3:** Build + test:
```bash
cd /Users/jason.yates79/Documents/GitHub/actions/rootio
go build ./... && go test ./...
```
Expected: PASS.

**Step 4:** Commit:
```bash
git add rootio/lvm/lvm.go rootio/storage/lvm.go
git commit -m "$(cat <<'EOF'
rootio: add idempotent LVM teardown helpers

Adds DeactivateVolumeGroup / RemoveVolumeGroup / RemovePhysicalVolume
wrappers in the lvm package, plus storage.TeardownVolumeGroups that
walks metadata in preflight order (vgchange -an, vgremove -ff per VG,
then pvremove -ff per PV).

All best-effort (non-zero exit ignored) so re-runs against hosts in
varied states succeed. Mirrors the StopRAID + ZeroSuperblock pattern
already used in raid.go.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: rootio — wire teardown into wipe + partition

**Files:**
- Modify: `rootio/cmd/rootio.go`

**Step 1:** In both `rootioPartition.Run` and `rootioWipe.Run`, insert the LVM teardown loop **before** the existing `StopRAID` loop:

```go
// LVM preflight: deactivate + remove VGs and wipe PV signatures
// before we tear down the RAID arrays they may sit on top of.
storage.TeardownVolumeGroups(metadata.Instance.Storage.VolumeGroups)

for _, r := range metadata.Instance.Storage.RAID {
    if err := storage.StopRAID(r.Name); err != nil {
        log.Error(err)
    }
    // ... existing body ...
}
```

**Step 2:** Build:
```bash
cd /Users/jason.yates79/Documents/GitHub/actions/rootio
go build ./...
```
Expected: PASS.

**Step 3:** Commit:
```bash
git add rootio/cmd/rootio.go
git commit -m "$(cat <<'EOF'
rootio: run LVM teardown before StopRAID in wipe/partition

TeardownVolumeGroups must run while the backing RAID arrays are still
assembled — pvremove on a stopped array is a no-op and leaves stale
LVM signatures on the member disks, breaking idempotency on re-runs.

Applied to both wipe and partition entry points, mirroring the RAID
preflight ordering.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: rootio — lvm.json fixture + README

**Files:**
- Create: `rootio/test/lvm.json`
- Modify: `rootio/README.md`

**Step 1:** Create `rootio/test/lvm.json` modeled on `test/raid.json`, with two disks → EFI + LINUX → md0 → vg0 → root/var/home/docker LVs + all filesystems. Match the expected metadata shape from the design doc (§4.4).

**Step 2:** Run the test harness against the fixture (sanity check, does not assert against a real system):
```bash
cd /Users/jason.yates79/Documents/GitHub/actions/rootio
TEST=1 JSON_FILE=test/lvm.json go run . version
```
Expected: Prints rootio version info + "Successfully parsed the MetaData, Found [2] Disks".

**Step 3:** Update `rootio/README.md` — add an "LVM on top of RAID" section after the RAID section, with a worked example pointing at `test/lvm.json`. Document the size-units quirk (partitions in sectors, LV size in bytes).

**Step 4:** Commit:
```bash
git add rootio/test/lvm.json rootio/README.md
git commit -m "$(cat <<'EOF'
rootio: add LVM-on-RAID test fixture + README section

test/lvm.json: two-disk EFI+LINUX layout assembled into md0, one vg0
PV on /dev/md0, four LVs (root/var/home/docker) with one size=0 fill
LV, and matching filesystems for each LV plus per-disk ESPs.

README: document the LVM section alongside the existing RAID docs,
including the size-units inconsistency (partitions in sectors,
LogicalVolume.Size in bytes — reflects the Equinix CPR convention).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: rootio — trigger image rebuild

**Files:** none (CI-driven)

**Step 1:** Push the branch:
```bash
cd /Users/jason.yates79/Documents/GitHub/actions
git push -u origin feat/root-on-lvm
```

**Step 2:** The release workflow `.github/workflows/release-ghcr.yml` (amd64-only, GHA cache) should publish `ghcr.io/jasonyates/tinkerbell-actions/rootio:<sha>` and `:latest`. Wait for it to finish before moving on — Tasks 15+ need the new image.

Confirm via:
```bash
gh run list --workflow release-ghcr.yml --limit 1
```

---

## Task 14: scripts — lvm2 in rootfs tarball

**Files:**
- Modify: `scripts/build-noble-rootfs-tarball.sh`

**Step 1:** Add `lvm2` and `thin-provisioning-tools` to both package install lines (EFI, BIOS). Example — before/after for one line:

```diff
-    mdadm grub-common grub-efi-amd64 grub-efi-amd64-bin \\
+    mdadm lvm2 thin-provisioning-tools grub-common grub-efi-amd64 grub-efi-amd64-bin \\
```

Same insertion for the BIOS variant.

**Step 2:** Commit:
```bash
git add scripts/build-noble-rootfs-tarball.sh
git commit -m "$(cat <<'EOF'
scripts: bundle lvm2 + thin-provisioning-tools in rootfs tarball

Root-on-LVM needs lvm2 installed in the target rootfs so update-initramfs
registers the lvm2 hook and the initrd activates VGs at boot.
thin-provisioning-tools silences an update-initramfs warning (lvm2
recommends it) even though we're not using thin provisioning.

Adds both to the EFI and BIOS package lists alongside mdadm so the
boot path sequence is: initramfs assembles md0 -> scans PVs -> activates
vg0 -> mounts LABEL=ROOT.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

**Step 3:** Rebuild + republish:
```bash
bash scripts/build-noble-rootfs-tarball.sh    # (or whatever the repo's build invocation is)
# upload resulting noble-rootfs-efi.tar.gz to http://31.24.228.5:7173/
```
(The publishing mechanism depends on how your build host pushes to the mirror. Check with the user if unclear.)

---

## Task 15: template — ubuntu-noble-lvm-raid1-efi.yaml

**Files:**
- Create: `docs/templates/ubuntu-noble-lvm-raid1-efi.yaml`

**Step 1:** Start by duplicating the existing RAID template:
```bash
cp docs/templates/ubuntu-noble-raid1-efi.yaml docs/templates/ubuntu-noble-lvm-raid1-efi.yaml
```

**Step 2:** Edit the new file:

- Top-of-file `metadata.name` and `metadata.labels.pretty-name`: change to `ubuntu-noble-lvm-raid1-efi`.
- Every `DEST_DISK: "/dev/md0"` → `DEST_DISK: "/dev/vg0/root"`
- Every `BLOCK_DEVICE: "/dev/md0"` → `BLOCK_DEVICE: "/dev/vg0/root"`
- Insert a new `configure-grub-lvm` step **immediately before** `update-initramfs`:

```yaml
- name: "configure-grub-lvm"
  image: ghcr.io/jasonyates/tinkerbell-actions/cexec:latest
  timeout: 60
  environment:
    BLOCK_DEVICE: "/dev/vg0/root"
    FS_TYPE: "ext4"
    CHROOT: "y"
    DEFAULT_INTERPRETER: "/bin/bash -eux -c"
    CMD_LINE: "echo 'GRUB_PRELOAD_MODULES=\"lvm mdraid1x\"' >> /etc/default/grub"
```

- `configure-fstab` `CMD_LINE`: replace the two-entry fstab with the six-entry LVM variant from the design doc (§4.5).

**Step 3:** Commit:
```bash
git add docs/templates/ubuntu-noble-lvm-raid1-efi.yaml
git commit -m "$(cat <<'EOF'
templates: add ubuntu-noble-lvm-raid1-efi for root-on-LVM-on-RAID1

Clone of the raid1-efi template with mount targets switched from
/dev/md0 to /dev/vg0/root, a new configure-grub-lvm step that
preloads the lvm + mdraid1x modules into grub for defensive boot,
and a six-entry LABEL-based fstab covering root, var, home, docker,
and both ESPs.

Expected matching metadata shape is documented in
docs/plans/2026-04-22-root-on-lvm-design.md §4.4.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: end-to-end validation

**Files:** none (live test)

**Step 1:** On a Tinkerbell cluster running the forked tinkerbell image from Task 6:
1. Apply the new Template CR from Task 15.
2. Create/update a Hardware CR with the metadata shape from §4.4 of the design doc.
3. Trigger a Workflow against a test host (e.g. `eu-lon1-control-001` or a spare).

**Step 2:** Watch the workflow progress:
```bash
kubectl -n tink-system get workflows -w
```
Expected states in order: `rootio-partition` → `rootio-format` → `extract-rootfs` → ... → `grub-install-sdb` → `register-efi-boot-entries` → `reboot` → all green.

**Step 3:** After first boot, SSH in and verify:
```bash
cat /proc/mdstat                # md0 active, raid1, [UU]
vgs                             # vg0 with 4 LVs
lvs                             # root/var/home/docker present
mount | grep -E 'ROOT|VAR|HOME|DOCKER|EFI'
cat /etc/fstab                  # 6 LABEL-based entries
```

**Step 4:** Reboot test:
```bash
reboot
# wait, SSH back in
uptime                          # confirms it came back
```

**Step 5:** Second-disk failure simulation (optional but recommended):
```bash
# on the test host, simulate sdb failure
mdadm --fail /dev/md0 /dev/sdb2
mdadm --remove /dev/md0 /dev/sdb2
reboot
# host should still boot from sda's ESP + degraded md0
```

If any step fails, apply the debugging skill to root-cause before patching; this plan assumes step 16.3 passes cleanly on the first try because each prior task has been independently verified.

---

## Merge strategy

- tinkerbell: open PR from `feat/metadata-lvm-passthrough` → `main` after Task 6.
- actions: open PR from `feat/root-on-lvm` → `main` (or into the existing `feat/rootio-sw-raid` if preferred, since this branch was cut from there) after Task 15.

The two PRs are independent — tinkerbell can merge first, actions after.
