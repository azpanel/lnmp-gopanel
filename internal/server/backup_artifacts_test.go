package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/azpanel/lnmp-gopanel/internal/auth"
	"github.com/azpanel/lnmp-gopanel/internal/backups"
)

func TestBackupArtifactQueryContracts(t *testing.T) {
	s, _ := testServer(t)
	s.backupScanSlots = make(chan struct{}, 2)
	b := &policyTestBackend{}
	s.BackupPolicies = b
	h := s.Handler()
	id, _ := s.Sessions.Create()
	cookie := &http.Cookie{Name: auth.CookieName, Value: id}
	policy := strings.Repeat("a", 32)
	endpoint := "/api/v1/backups/policies/" + policy + "/artifacts"
	for _, suffix := range []string{"", "?revision=1", "?revision=0&destination_index=0", "?revision=1&destination_index=-1", "?revision=1&destination_index=100", "?revision=1&destination_index=x", "?revision=1&destination_index=0&path=/etc", "?revision=1&revision=2&destination_index=0"} {
		w := request(h, "GET", endpoint+suffix, "", cookie, "", "")
		if w.Code != 400 {
			t.Fatal(suffix, w.Code, w.Body.String())
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid query reached helper")
	}
	suffix := "?revision=7&destination_index=0"
	if w := request(h, "GET", endpoint+suffix, "", nil, "", ""); w.Code != 401 {
		t.Fatal("unauthenticated lookup", w.Code)
	}
	if w := request(h, "GET", endpoint+suffix, "", cookie, "", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	q := b.input.(backups.ArtifactQuery)
	if b.action != "artifacts" || q.ID != policy || q.Revision != 7 || q.DestinationIndex == nil || *q.DestinationIndex != 0 {
		t.Fatal(b.action, q)
	}
	s.backupScanSlots <- struct{}{}
	s.backupScanSlots <- struct{}{}
	if w := request(h, "GET", endpoint+suffix, "", cookie, "", ""); w.Code != 429 {
		t.Fatal("concurrent query limit", w.Code)
	}
}
