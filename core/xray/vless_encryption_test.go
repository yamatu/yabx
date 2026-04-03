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

func TestResolveVlessInboundDecryptionWrapsRawX25519Key(t *testing.T) {
	node := &panel.VAllssNode{
		Encryption: "1N2wG4m8g8xv8cRXX8P8aNqL2vW4M2LwF7p0M8l8wSU",
		Decryption: "CFYGW1MRvmQFdqqyncKo7cWcY3nUfH5HpOv3nR5ednQ",
	}

	got := resolveVlessInboundDecryption(node)
	want := "mlkem768x25519plus.native.600s.CFYGW1MRvmQFdqqyncKo7cWcY3nUfH5HpOv3nR5ednQ"
	if got != want {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, want)
	}
}

func TestResolveVlessOutboundEncryptionWrapsRawX25519Key(t *testing.T) {
	node := &panel.VAllssNode{
		Encryption: "1N2wG4m8g8xv8cRXX8P8aNqL2vW4M2LwF7p0M8l8wSU",
		Decryption: "CFYGW1MRvmQFdqqyncKo7cWcY3nUfH5HpOv3nR5ednQ",
	}

	got := resolveVlessOutboundEncryption(node)
	want := "mlkem768x25519plus.native.0rtt.1N2wG4m8g8xv8cRXX8P8aNqL2vW4M2LwF7p0M8l8wSU"
	if got != want {
		t.Fatalf("resolveVlessOutboundEncryption() = %q, want %q", got, want)
	}
}

func TestResolveVlessEncryptionPreservesStructuredValue(t *testing.T) {
	structuredEnc := "mlkem768x25519plus.native.0rtt.some-key"
	structuredDec := "mlkem768x25519plus.native.600s.some-key"
	node := &panel.VAllssNode{
		Encryption: structuredEnc,
		Decryption: structuredDec,
	}

	if got := resolveVlessOutboundEncryption(node); got != structuredEnc {
		t.Fatalf("resolveVlessOutboundEncryption() = %q, want %q", got, structuredEnc)
	}
	if got := resolveVlessInboundDecryption(node); got != structuredDec {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, structuredDec)
	}
}
