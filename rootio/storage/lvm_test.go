package storage

import (
	"encoding/json"
	"reflect"
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
