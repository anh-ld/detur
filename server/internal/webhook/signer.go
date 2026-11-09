package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func Sign(payload []byte, secret string, timestamp int64) string {
	return fmt.Sprintf("t=%d,v1=%s", timestamp, signatureHex(payload, secret, timestamp))
}

func signatureHex(payload []byte, secret string, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "t.%d.", timestamp)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(payload []byte, secret, header string, tolerance time.Duration) bool {
	var tStr, sigStr string
	for _, p := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			tStr = v
		case "v1":
			sigStr = v
		}
	}
	if tStr == "" || sigStr == "" {
		return false
	}

	ts, err := strconv.ParseInt(tStr, 10, 64)
	if err != nil {
		return false
	}

	if tolerance > 0 {
		diff := time.Since(time.Unix(ts, 0))
		if diff < -tolerance || diff > tolerance {
			return false
		}
	}

	return subtle.ConstantTimeCompare([]byte(signatureHex(payload, secret, ts)), []byte(sigStr)) == 1
}
