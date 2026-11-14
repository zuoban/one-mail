package access

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestAccess(t *testing.T, password string) *Access {
	t.Helper()
	a, err := New(password)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPasswordModes(t *testing.T) {
	open := newTestAccess(t, "")
	if open.PasswordRequired() {
		t.Fatal("empty password must disable authentication")
	}
	for _, header := range []string{"", "Bearer broken", "Basic old-password", "old-user-token"} {
		if !open.Authorized(header) {
			t.Fatalf("password-free mode rejected header %q", header)
		}
	}
	if token, err := open.Login("anything"); err != nil || token != "" {
		t.Fatal("password-free mode should not require or issue a session")
	}

	for _, password := range []string{"instance-password", " ", "  padded password\t"} {
		t.Run(password, func(t *testing.T) {
			a := newTestAccess(t, password)
			if !a.PasswordRequired() {
				t.Fatal("nonempty password must enable authentication")
			}
			for _, wrong := range []string{"", "wrong", strings.TrimSpace(password) + "wrong"} {
				if token, err := a.Login(wrong); err == nil || token != "" {
					t.Fatal("incorrect password issued a session")
				}
			}
			if password != strings.TrimSpace(password) {
				if _, err := a.Login(strings.TrimSpace(password)); err == nil {
					t.Fatal("password whitespace must be preserved")
				}
			}
			token, err := a.Login(password)
			if err != nil || token == "" {
				t.Fatalf("correct password failed: %v", err)
			}
			for _, header := range []string{"Bearer " + token, "bearer " + token} {
				if !a.Authorized(header) {
					t.Fatal("valid session rejected")
				}
			}
			for _, header := range []string{"", "Bearer broken", token, "Basic " + token, "Bearer " + token + " extra"} {
				if a.Authorized(header) {
					t.Fatalf("invalid authorization %q accepted", header)
				}
			}
		})
	}
}

func TestIssuedSessionClaims(t *testing.T) {
	a := newTestAccess(t, "instance-password")
	tokenString, err := a.Login("instance-password")
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return a.signingKey[:], nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		t.Fatalf("issued token is invalid: %v", err)
	}
	if len(claims) != 5 || claims["iss"] != sessionIssuer || claims["sub"] != sessionSubject {
		t.Fatalf("unexpected session claims: %v", claims)
	}
	for _, key := range []string{"user_id", "username", "session_version", "password"} {
		if _, exists := claims[key]; exists {
			t.Fatalf("issued token contains %q", key)
		}
	}
	expires, err := claims.GetExpirationTime()
	if err != nil || expires == nil {
		t.Fatalf("missing expiration: %v", err)
	}
	issued, err := claims.GetIssuedAt()
	if err != nil || issued == nil || expires.Sub(issued.Time) != sessionLifetime {
		t.Fatalf("session must expire seven days after issue: %v", err)
	}
}

func TestRejectsInvalidSessions(t *testing.T) {
	a := newTestAccess(t, "instance-password")
	now := time.Now()
	valid := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": sessionIssuer,
			"aud": sessionAudience,
			"sub": sessionSubject,
			"iat": now.Unix(),
			"exp": now.Add(time.Hour).Unix(),
		}
	}
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		change func(jwt.MapClaims)
	}{
		{"HS384", jwt.SigningMethodHS384, nil},
		{"HS512", jwt.SigningMethodHS512, nil},
		{"unsigned", jwt.SigningMethodNone, nil},
		{"expired", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Hour).Unix() }},
		{"missing expiry", jwt.SigningMethodHS256, func(c jwt.MapClaims) { delete(c, "exp") }},
		{"wrong issuer", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["iss"] = "another-app" }},
		{"missing issuer", jwt.SigningMethodHS256, func(c jwt.MapClaims) { delete(c, "iss") }},
		{"wrong audience", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["aud"] = "another-app" }},
		{"missing audience", jwt.SigningMethodHS256, func(c jwt.MapClaims) { delete(c, "aud") }},
		{"wrong subject", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["sub"] = "another-session" }},
		{"future issued", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["iat"] = now.Add(time.Hour).Unix() }},
		{"legacy user session", jwt.SigningMethodHS256, func(c jwt.MapClaims) {
			delete(c, "iss")
			delete(c, "aud")
			delete(c, "sub")
			c["user_id"] = 1
			c["username"] = "old-user"
			c["session_version"] = 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := valid()
			if tc.change != nil {
				tc.change(claims)
			}
			var key interface{} = a.signingKey[:]
			if tc.method == jwt.SigningMethodNone {
				key = jwt.UnsafeAllowNoneSignatureType
			}
			token, err := jwt.NewWithClaims(tc.method, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if a.Authorized("Bearer " + token) {
				t.Fatal("invalid session accepted")
			}
		})
	}
	claims := valid()
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("unrelated-signing-key"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Authorized("Bearer " + forged) {
		t.Fatal("invalid signature accepted")
	}
}

func TestNewInstancesRejectPreviousSessions(t *testing.T) {
	first := newTestAccess(t, "original-password")
	token, err := first.Login("original-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"original-password", "changed-password"} {
		next := newTestAccess(t, password)
		if next.Authorized("Bearer " + token) {
			t.Fatal("a new instance accepted a previous instance's session")
		}
	}
}
