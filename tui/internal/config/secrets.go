package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

)

// GenerateValue generates a value for the given key.
// allValues is the current flat map of env values; it may be mutated when
// JWT_SECRET must be generated first to satisfy ANON_KEY or SERVICE_ROLE_KEY.
func GenerateValue(key string, allValues map[string]string) (string, error) {
	switch key {
	case "JWT_SECRET":
		v := randomBase64(32)
		allValues[key] = v
		return v, nil

	case "ANON_KEY", "SERVICE_ROLE_KEY":
		secret := allValues["JWT_SECRET"]
		if secret == "" || placeholderRe.MatchString(secret) {
			// Generate JWT_SECRET first (trap from AGENTS.md)
			secret = randomBase64(32)
			allValues["JWT_SECRET"] = secret
		}
		role := "anon"
		if key == "SERVICE_ROLE_KEY" {
			role = "service_role"
		}
		return generateJWT(role, secret)

	case "SECRET_KEY_BASE":
		return randomHex(64), nil
	case "VAULT_ENC_KEY":
		return randomAlphaNum(32), nil
	case "PG_META_CRYPTO_KEY":
		return randomHex(32), nil

	case "LOGFLARE_PUBLIC_ACCESS_TOKEN":
		return newUUID(), nil
	case "LOGFLARE_PRIVATE_ACCESS_TOKEN":
		return newUUID(), nil

	case "S3_PROTOCOL_ACCESS_KEY_ID":
		return randomBase64(20), nil
	case "S3_PROTOCOL_ACCESS_KEY_SECRET":
		return randomBase64(40), nil

	case "DASHBOARD_PASSWORD":
		return randomBase64(20), nil
	case "POSTGRES_PASSWORD":
		return randomBase64(32), nil
	case "SMTP_PASS":
		return randomBase64(20), nil
	case "MINIO_ROOT_PASSWORD":
		return randomBase64(20), nil
	}

	return "", fmt.Errorf("no generation rule for key: %s", key)
}

// generateJWT creates an HS256 JWT with the given role claim and a 10-year expiry.
func generateJWT(role, secret string) (string, error) {
	header := base64URLEncode(mustJSON(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}))

	now := time.Now()
	payload := base64URLEncode(mustJSON(map[string]interface{}{
		"role": role,
		"iss":  "supabase",
		"iat":  now.Unix(),
		"exp":  now.Add(10 * 365 * 24 * time.Hour).Unix(),
	}))

	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := base64URLEncodeBytes(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func base64URLEncode(data []byte) string {
	s := base64.StdEncoding.EncodeToString(data)
	s = strings.ReplaceAll(s, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.TrimRight(s, "=")
	return s
}

func base64URLEncodeBytes(data []byte) string {
	return base64URLEncode(data)
}

func randomBase64(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomAlphaNum(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i, v := range b {
		b[i] = charset[int(v)%len(charset)]
	}
	return string(b)
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
