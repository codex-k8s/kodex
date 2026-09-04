package platform

import "testing"

func TestRuntimeSecretCursorBindsProjectAndQuery(t *testing.T) {
	token, err := encodeRuntimeSecretListCursor("prj_example", "database", "sec_example")
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	cursor, err := decodeRuntimeSecretListCursor(token, "prj_example", "database")
	if err != nil || cursor.Ref != "sec_example" {
		t.Fatalf("decode cursor: cursor=%#v err=%v", cursor, err)
	}
	for _, test := range []struct{ project, query string }{{"prj_other", "database"}, {"prj_example", "token"}} {
		if _, err := decodeRuntimeSecretListCursor(token, test.project, test.query); err == nil {
			t.Fatalf("cursor accepted for another filter: %#v", test)
		}
	}
}
