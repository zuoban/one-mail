package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
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
	secret := getJWTSecret()
	if secret == defaultSecret {
		log.Println("⚠️  警告: 使用默认 JWT 密钥，生产环境请设置环境变量 JWT_SECRET 或配置文件 jwt.secret")
	}
	jwtSecret = []byte(secret)

	// 解析过期时间
	expiresStr := "168h" // 默认 7 天
	if config.AppConfig != nil && config.AppConfig.JWT.Expires != "" {
		expiresStr = config.AppConfig.JWT.Expires
	}

	var err error
	jwtExpires, err = time.ParseDuration(expiresStr)
	if err != nil {
		jwtExpires = 168 * time.Hour
		log.Printf("警告: 无效的 JWT 过期时间配置，使用默认值 168h: %v", err)
	}

	return nil
}

// getJWTSecret 获取 JWT 密钥，优先级: 环境变量 > 配置文件 > 默认值
func getJWTSecret() string {
	// 1. 优先从环境变量获取
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}

	// 2. 从配置文件获取
	if config.AppConfig != nil && config.AppConfig.JWT.Secret != "" {
		return config.AppConfig.JWT.Secret
	}

	// 3. 使用默认值（仅开发环境）
	return defaultSecret
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
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uint, username string) (string, error) {
	if len(jwtSecret) == 0 {
		return "", errors.New("JWT 未初始化，请先调用 InitJWT()")
	}

	expirationTime := time.Now().Add(jwtExpires)
	claims := &Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func ValidateToken(tokenString string) (*Claims, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("JWT 未初始化，请先调用 InitJWT()")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
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