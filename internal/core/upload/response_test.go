package upload

import "testing"

func TestValidateUploadServers(t *testing.T) {
	for _, servers := range [][]string{nil, {}, {""}, {"  "}} {
		if validateUploadServers(servers) == nil {
			t.Fatalf("accepted malformed server metadata: %v", servers)
		}
	}
	if err := validateUploadServers([]string{"store1"}); err != nil {
		t.Fatal(err)
	}
}
