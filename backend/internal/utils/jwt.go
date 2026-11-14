package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"one-mail/backend/config"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const defaultSecret = "one-mail-secret-key-change-in-production"

var jwtSecret []byte
var jwtExpires time.Duration

// InitJWT 初始化 JWT 配置，应在程序启动时调用
func InitJWT() error {
	jwtSecret = nil
	jwtExpires = 0
	secret := getJWTSecret()
	if len(secret) < 32 || strings.TrimSpace(secret) == "" || secret == defaultSecret ||
		secret == "916481d3-a196-46d1-a05b-fea0a6984c04" || secret == "your-secret-key-change-in-production" {
		return errors.New("JWT_SECRET or jwt.secret must contain at least 32 bytes and must not use a default secret")
	}

	expiresStr := "168h"
	if config.AppConfig != nil && config.AppConfig.JWT.Expires != "" {
		expiresStr = config.AppConfig.JWT.Expires
	}
	expires, err := time.ParseDuration(expiresStr)
	if err != nil || expires <= 0 {
		return errors.New("jwt.expires must be a positive duration")
	}

	jwtSecret = []byte(secret)
	jwtExpires = expires
	return nil
}

// getJWTSecret 获取 JWT 密钥，优先级: 环境变量 > 配置文件。
func getJWTSecret() string {
	if secret, exists := os.LookupEnv("JWT_SECRET"); exists {
		return secret
	}
	if config.AppConfig != nil {
		return config.AppConfig.JWT.Secret
	}
	return ""
}

// GenerateRandomSecret 生成随机密钥（用于生产环境初始化）
func GenerateRandomSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

type Claims struct {
	UserID         uint   `json:"user_id"`
	Username       string `json:"username"`
	SessionVersion uint   `json:"session_version"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uint, username string, sessionVersion uint) (string, error) {
	if len(jwtSecret) == 0 {
		return "", errors.New("JWT is not initialized")
	}
	if userID == 0 || sessionVersion == 0 {
		return "", errors.New("invalid user session")
	}

	now := time.Now()
	claims := &Claims{
		UserID:         userID,
		Username:       username,
		SessionVersion: sessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(jwtExpires)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

func ValidateToken(tokenString string) (*Claims, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("JWT is not initialized")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.UserID == 0 || claims.SessionVersion == 0 {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
