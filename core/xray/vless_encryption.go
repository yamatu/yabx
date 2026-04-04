package xray

import (
	"encoding/base64"
	"strings"

	"github.com/InazumaV/V2bX/api/panel"
)

const (
	vlessEncryptionPrefix             = "mlkem768x25519plus."
	vlessEncryptionInboundMode        = "mlkem768x25519plus.native.600s."
	vlessEncryptionOutboundMode       = "mlkem768x25519plus.native.0rtt."
	vlessEncryptionX25519KeySize      = 32
	vlessEncryptionMlkemSeedSize      = 64
	vlessEncryptionMlkemCipherKeySize = 1184
)

func resolveVlessInboundDecryption(v *panel.VAllssNode) string {
	if v == nil || !v.HasVlessEncryption() {
		return "none"
	}

	decryption := v.NormalizedDecryption()
	if decryption == "" {
		return "none"
	}

	if isStructuredVlessEncryptionValue(decryption) {
		return decryption
	}

	if isRawBase64URLKey(decryption, vlessEncryptionX25519KeySize, vlessEncryptionMlkemSeedSize) {
		return vlessEncryptionInboundMode + decryption
	}

	return decryption
}

func resolveVlessOutboundEncryption(v *panel.VAllssNode) string {
	if v == nil || !v.HasVlessEncryption() {
		return ""
	}

	encryption := v.NormalizedEncryption()
	if encryption == "" {
		return ""
	}

	if isStructuredVlessEncryptionValue(encryption) {
		return encryption
	}

	if isRawBase64URLKey(encryption, vlessEncryptionX25519KeySize, vlessEncryptionMlkemCipherKeySize) {
		return vlessEncryptionOutboundMode + encryption
	}

	return encryption
}

func isStructuredVlessEncryptionValue(value string) bool {
	return strings.HasPrefix(value, vlessEncryptionPrefix)
}

func isRawBase64URLKey(value string, expectedLens ...int) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	for _, expectedLen := range expectedLens {
		if len(decoded) == expectedLen {
			return true
		}
	}
	return false
}
