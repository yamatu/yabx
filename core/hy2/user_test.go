package hy2

import (
	"fmt"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	vCore "github.com/InazumaV/V2bX/core"
)

// TestHysteria2AddAndDelUsers covers the single pass update of the shared user
// map: every user is added (and removed) in one locked loop instead of one
// goroutine per user.
func TestHysteria2AddAndDelUsers(t *testing.T) {
	h := &Hysteria2{Auth: &V2bX{usersMap: make(map[string]int)}}
	users := make([]panel.UserInfo, 0, 100)
	for i := range 100 {
		users = append(users, panel.UserInfo{Id: i + 1, Uuid: fmt.Sprintf("uuid-%d", i)})
	}

	added, err := h.AddUsers(&vCore.AddUsersParams{Tag: "test", Users: users})
	if err != nil {
		t.Fatalf("AddUsers error: %s", err)
	}
	if added != len(users) {
		t.Errorf("AddUsers added %d users, want %d", added, len(users))
	}
	if len(h.Auth.usersMap) != len(users) {
		t.Fatalf("user map holds %d users, want %d", len(h.Auth.usersMap), len(users))
	}
	if ok, id := h.Auth.Authenticate(nil, "uuid-99", 0); !ok || id != "uuid-99" {
		t.Errorf("Authenticate(uuid-99) = %v/%q, want true/uuid-99", ok, id)
	}
	if ok, _ := h.Auth.Authenticate(nil, "uuid-100", 0); ok {
		t.Error("a user that was never added authenticated")
	}

	if err := h.DelUsers(users, "test", nil); err != nil {
		t.Fatalf("DelUsers error: %s", err)
	}
	if len(h.Auth.usersMap) != 0 {
		t.Errorf("user map still holds %d users after DelUsers", len(h.Auth.usersMap))
	}
	if ok, _ := h.Auth.Authenticate(nil, "uuid-0", 0); ok {
		t.Error("a deleted user still authenticated")
	}
}
