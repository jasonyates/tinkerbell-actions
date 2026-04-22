package storage

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
	return nil
}

func CreateVolumeGroup(volumeGroup VolumeGroup) error {
	if err := ValidateVolumeGroup(volumeGroup); err != nil {
		return err
	}

	for _, p := range volumeGroup.PhysicalVolumes {
		if err := lvm.CreatePhysicalVolume(p); err != nil {
			return fmt.Errorf("failed to create physical volume %s: %w", p, err)
		}
	}

	vg, err := lvm.CreateVolumeGroup(volumeGroup.Name, volumeGroup.PhysicalVolumes, volumeGroup.Tags)
	if err != nil {
		return fmt.Errorf("failed to create volume group %s: %w", volumeGroup.Name, err)
	}

	for _, lv := range volumeGroup.LogicalVolumes {
		if err := vg.CreateLogicalVolume(lv.Name, lv.Size, lv.Tags, lv.Opts); err != nil {
			return fmt.Errorf("failed to create logical volume %s: %w", lv.Name, err)
		}
	}

	return nil
}
