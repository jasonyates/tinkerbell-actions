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
		{"two fill LVs", VolumeGroup{Name: "vg0", PhysicalVolumes: []string{"/dev/md0"}, LogicalVolumes: []LogicalVolume{fillLV, fillLV}}, "last"},
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
