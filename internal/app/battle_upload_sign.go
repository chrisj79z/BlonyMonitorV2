package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"blonymonitorv2/internal/config"
)

func isUploadSecretConfigured() bool {
	secret := strings.TrimSpace(config.UploadSecret)
	if secret == "" || secret == config.UploadSecretPlaceholder {
		return false
	}
	return true
}

func newUploadNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashUploadPayload(gzData []byte) string {
	sum := sha256.Sum256(gzData)
	return hex.EncodeToString(sum[:])
}

func signBattleUpload(secret string, timestamp int64, nonce, playerID string, gzData []byte) string {
	payload := fmt.Sprintf("%d\n%s\n%s\n%s", timestamp, nonce, playerID, hashUploadPayload(gzData))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyBattleUploadSignature(secret string, timestamp int64, nonce, playerID string, gzData []byte, signature string) bool {
	expected := signBattleUpload(secret, timestamp, nonce, playerID, gzData)
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}
