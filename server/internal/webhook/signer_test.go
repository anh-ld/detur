package webhook

import (
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	secret := "whsec_testsecret1234567890abcdef"
	payload := []byte(`[{"type":"install","id":"inst_1"}]`)
	now := time.Now().Unix()

	sig := Sign(payload, secret, now)
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}

	// valid
	if !VerifySignature(payload, secret, sig, 5*time.Minute) {
		t.Error("expected signature verification to succeed")
	}

	// wrong secret fails
	if VerifySignature(payload, "wrong_secret", sig, 5*time.Minute) {
		t.Error("expected signature with wrong secret to fail")
	}

	// tampered payload fails
	tampered := []byte(`[{"type":"install","id":"inst_2"}]`)
	if VerifySignature(tampered, secret, sig, 5*time.Minute) {
		t.Error("expected tampered payload to fail verification")
	}

	// expired timestamp fails tolerance
	oldTime := time.Now().Add(-10 * time.Minute).Unix()
	oldSig := Sign(payload, secret, oldTime)
	if VerifySignature(payload, secret, oldSig, 5*time.Minute) {
		t.Error("expected old signature to fail 5-minute tolerance check")
	}
}
