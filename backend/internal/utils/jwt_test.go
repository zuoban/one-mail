package utils

import (
	"os"
	"strings"
	"testing"
	"time"

	"one-mail/backend/config"

	"github.com/golang-jwt/jwt/v5"
)

func preserveJWTConfig(t *testing.T) {
	t.Helper()
	oldConfig, oldSecret, oldExpires := config.AppConfig, jwtSecret, jwtExpires
	t.Cleanup(func() {
		config.AppConfig, jwtSecret, jwtExpires = oldConfig, oldSecret, oldExpires
	})
}

func TestInitJWTRejectsUnsafeSecrets(t *testing.T) {
	preserveJWTConfig(t)
	config.AppConfig = &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("c", 32)}}
	for _, secret := range []string{"", "short", strings.Repeat(" ", 32), defaultSecret, "916481d3-a196-46d1-a05b-fea0a6984c04", "your-secret-key-change-in-production"} {
		t.Run(secret, func(t *testing.T) {
			t.Setenv("JWT_SECRET", secret)
			jwtSecret = []byte(strings.Repeat("previous-key", 4))
			if err := InitJWT(); err == nil {
				t.Fatal("expected unsafe secret to be rejected")
			}
			if _, err := GenerateToken(1, "alice", 1); err == nil {
				t.Fatal("failed initialization must not retain the previous key")
			}
		})
	}
}

func TestInitJWTConfigAndExpiry(t *testing.T) {
	preserveJWTConfig(t)
	t.Setenv("JWT_SECRET", strings.Repeat("e", 32))
	config.AppConfig = &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("c", 32), Expires: "2h"}}
	if err := InitJWT(); err != nil {
		t.Fatal(err)
	}
	if string(jwtSecret) != strings.Repeat("e", 32) || jwtExpires != 2*time.Hour {
		t.Fatal("environment secret or configured expiry was not used")
	}
	if err := os.Unsetenv("JWT_SECRET"); err != nil {
		t.Fatal(err)
	}
	if err := InitJWT(); err != nil || string(jwtSecret) != strings.Repeat("c", 32) {
		t.Fatal("YAML secret should be used when the environment variable is absent")
	}
	for _, expires := range []string{"invalid", "0s", "-1h"} {
		config.AppConfig.JWT.Expires = expires
		if err := InitJWT(); err == nil {
			t.Fatalf("expected expiry %q to fail", expires)
		}
	}
	config.AppConfig = nil
	if err := InitJWT(); err == nil {
		t.Fatal("missing environment and YAML secret must fail")
	}
}

func TestJWTRequiresHS256ExpiryAndSessionVersion(t *testing.T) {
	preserveJWTConfig(t)
	t.Setenv("JWT_SECRET", strings.Repeat("k", 32))
	config.AppConfig = &config.Config{JWT: config.JWTConfig{Expires: "1h"}}
	if err := InitJWT(); err != nil {
		t.Fatal(err)
	}
	token, err := GenerateToken(7, "alice", 4)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateToken(token)
	if err != nil || claims.UserID != 7 || claims.SessionVersion != 4 || claims.ExpiresAt == nil {
		t.Fatalf("valid token did not preserve session: claims=%+v err=%v", claims, err)
	}
	for _, tc := range []struct {
		name    string
		method  jwt.SigningMethod
		expires *jwt.NumericDate
		version uint
	}{
		{"wrong algorithm", jwt.SigningMethodHS384, jwt.NewNumericDate(time.Now().Add(time.Hour)), 1},
		{"missing expiry", jwt.SigningMethodHS256, nil, 1},
		{"expired", jwt.SigningMethodHS256, jwt.NewNumericDate(time.Now().Add(-time.Hour)), 1},
		{"missing session", jwt.SigningMethodHS256, jwt.NewNumericDate(time.Now().Add(time.Hour)), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := &Claims{UserID: 7, Username: "alice", SessionVersion: tc.version,
				RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: tc.expires}}
			signed, err := jwt.NewWithClaims(tc.method, claims).SignedString(jwtSecret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateToken(signed); err == nil {
				t.Fatal("expected invalid token to be rejected")
			}
		})
	}
	if _, err := GenerateToken(7, "alice", 0); err == nil {
		t.Fatal("zero session version must not produce a token")
	}
}
