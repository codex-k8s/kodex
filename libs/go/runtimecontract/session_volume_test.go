package runtimecontract

import (
	"reflect"
	"testing"
)

func TestSessionVolumeMetadataBindsExactOwner(t *testing.T) {
	labels, annotations, err := SessionVolumeMetadata("org_abcdefgh", "", "ses_abcdefgh")
	if err != nil || labels["runtime.kodex.dev/managed"] != "true" || annotations["runtime.kodex.dev/project-hash"] != "e3b0c44298fc1c14" {
		t.Fatal("organization volume metadata is not canonical")
	}
	for _, owner := range [][3]string{{"org_other000", "", "ses_abcdefgh"}, {"org_abcdefgh", "prj_abcdefgh", "ses_abcdefgh"}, {"org_abcdefgh", "", "ses_other000"}} {
		otherLabels, otherAnnotations, err := SessionVolumeMetadata(owner[0], owner[1], owner[2])
		if err != nil || reflect.DeepEqual(labels, otherLabels) && reflect.DeepEqual(annotations, otherAnnotations) {
			t.Fatal("different owner reused canonical metadata")
		}
	}
	for _, owner := range [][3]string{{"", "", "ses_abcdefgh"}, {"org_abcdefgh", "bad", "ses_abcdefgh"}, {"org_abcdefgh", "", "../session"}} {
		if _, _, err := SessionVolumeMetadata(owner[0], owner[1], owner[2]); err == nil {
			t.Fatal("invalid owner was accepted")
		}
	}
}
