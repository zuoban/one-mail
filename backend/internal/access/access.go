package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	sessionIssuer   = "one-mail"
	sessionAudience = "one-mail-admin"
	sessionSubject  = "admin"
	sessionLifetime = 7 * 24 * time.Hour
)

// Access controls the optional instance password and its restart-scoped sessions.
type Access struct {
	passwordRequired bool
	passwordHash     [sha256.Size]byte
	signingKey       [32]byte
	parser           *jwt.Parser
}

func New(password string) (*Access, error) {
	a := &Access{passwordRequired: password != ""}
	if !a.passwordRequired {
		return a, nil
	}
	a.passwordHash = sha256.Sum256([]byte(password))
	if _, err := rand.Read(a.signingKey[:]); err != nil {
		return nil, err
	}
	a.parser = jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(sessionIssuer),
		jwt.WithAudience(sessionAudience),
		jwt.WithSubject(sessionSubject),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	return a, nil
}

func (a *Access) PasswordRequired() bool {
	return a.passwordRequired
}

func (a *Access) Authorized(authorization string) bool {
	if !a.passwordRequired {
		return true
	}
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	token, err := a.parser.ParseWithClaims(parts[1], &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return a.signingKey[:], nil
	})
	return err == nil && token.Valid
}

func (a *Access) Login(password string) (string, error) {
	if !a.passwordRequired {
		return "", nil
	}
	passwordHash := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(passwordHash[:], a.passwordHash[:]) != 1 {
		return "", errors.New("密码错误")
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    sessionIssuer,
		Subject:   sessionSubject,
		Audience:  jwt.ClaimStrings{sessionAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(sessionLifetime)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.signingKey[:])
}
