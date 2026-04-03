package xray

import (
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/xtls/xray-core/proxy/vless"
)

func TestBuildVlessUserPreservesEncryptionValue(t *testing.T) {
	user := panel.UserInfo{Uuid: "11111111-1111-1111-1111-111111111111"}

	protocolUser := buildVlessUser("test-tag", &user, "xtls-rprx-vision", "public-key")
	memoryUser, err := protocolUser.ToMemoryUser()
	if err != nil {
		t.Fatalf("ToMemoryUser() error = %v", err)
	}

	account, ok := memoryUser.Account.(*vless.MemoryAccount)
	if !ok {
		t.Fatalf("account type = %T, want *vless.MemoryAccount", memoryUser.Account)
	}

	if account.Encryption != "public-key" {
		t.Fatalf("Encryption = %q, want %q", account.Encryption, "public-key")
	}
}

func TestBuildVlessUserDisablesEncryptionWhenEmpty(t *testing.T) {
	user := panel.UserInfo{Uuid: "11111111-1111-1111-1111-111111111111"}

	protocolUser := buildVlessUser("test-tag", &user, "xtls-rprx-vision", "")
	memoryUser, err := protocolUser.ToMemoryUser()
	if err != nil {
		t.Fatalf("ToMemoryUser() error = %v", err)
	}

	account, ok := memoryUser.Account.(*vless.MemoryAccount)
	if !ok {
		t.Fatalf("account type = %T, want *vless.MemoryAccount", memoryUser.Account)
	}

	if account.Encryption != "" {
		t.Fatalf("Encryption = %q, want empty", account.Encryption)
	}
}
