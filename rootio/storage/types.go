package storage

import (
	"context"

	"github.com/tinkerbell/actions/pkg/metadata"
)

// Back-compat aliases so every caller that already imports
// rootio/storage continues to compile after we delete
// storage/metadata.go and migrate to pkg/metadata as the single
// source of truth for the wire format.
//
// Mount is deliberately omitted from the aliases because rootio's
// storage package already uses Mount as a function name
// (see mount.go); the metadata.Mount type is only ever reached
// through Filesystem.Mount, so nothing in rootio needs the
// bare type alias.
type (
	Metadata        = metadata.Metadata
	Instance        = metadata.Instance
	Filesystem      = metadata.Filesystem
	Disk            = metadata.Disk
	Partitions      = metadata.Partition // rootio historically singularised this type name as "Partitions"
	RAID            = metadata.RAID
	VolumeGroup     = metadata.VolumeGroup
	LogicalVolume   = metadata.LogicalVolume
	OperatingSystem = metadata.OperatingSystem
	Facility        = metadata.Facility
)

// Wrapper is the legacy JSON wrapper shape historically used by the
// rootio test fixtures ({"metadata": {...}}) and by cmd/rootio.go's
// TEST mode loader. Kept here so those callers compile unchanged.
type Wrapper struct {
	Metadata Metadata `json:"metadata"`
}

// RetrieveData fetches Hegel metadata via the shared pkg/metadata
// client. Kept at package level so cmd/rootio.go doesn't need
// restructuring. Takes a context so future callers can propagate
// cancellation; the rootio cobra commands currently pass
// context.Background().
func RetrieveData(ctx context.Context) (*Metadata, error) {
	c, err := metadata.New()
	if err != nil {
		return nil, err
	}
	return c.Fetch(ctx)
}
