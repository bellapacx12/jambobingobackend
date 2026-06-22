package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

// TelegramUser represents parsed initData user object
type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	Language  string `json:"language_code,omitempty"`
	IsPremium bool   `json:"is_premium,omitempty"`
}

// Claims represents JWT claims for authenticated users
type Claims struct {
	TelegramID int64  `json:"telegram_id"`
	Username   string `json:"username"`
	jwt.RegisteredClaims
}

// ValidateInitData validates Telegram WebApp initData using HMAC-SHA256
func ValidateInitData(botToken string) fiber.Handler {
	return func(c fiber.Ctx) error {
		initData := c.Get("X-Telegram-Init-Data", "")
		if initData == "" {
			initData = c.Query("initData", "")
		}
		if initData == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing init data",
			})
		}

		values, err := url.ParseQuery(initData)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid init data format",
			})
		}

		receivedHash := values.Get("hash")
		if receivedHash == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing hash signature",
			})
		}
		values.Del("hash")

		var keys []string
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var dataCheckParts []string
		for _, k := range keys {
			dataCheckParts = append(dataCheckParts, fmt.Sprintf("%s=%s", k, values.Get(k)))
		}
		dataCheckString := strings.Join(dataCheckParts, "\n")  // ← FIXED: was ""

		secretKey := hmacSHA256("WebAppData", botToken)
		computedHash := hex.EncodeToString(hmacSHA256Raw(secretKey, dataCheckString))

		if !hmac.Equal([]byte(computedHash), []byte(receivedHash)) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error":   "invalid init data signature",
				"message": "hash verification failed",
			})
		}

		authDate := values.Get("auth_date")
		if authDate != "" {
			var timestamp int64
			fmt.Sscanf(authDate, "%d", &timestamp)
			if time.Now().Unix()-timestamp > 86400 {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "init data expired",
				})
			}
		}

		userData := values.Get("user")
		if userData != "" {
			c.Locals("initData", initData)
			c.Locals("userData", userData)
		}

		return c.Next()
	}
}

// GenerateJWT creates a JWT token for authenticated Telegram users
func GenerateJWT(telegramID int64, username string, secret string) (string, error) {
	claims := Claims{
		TelegramID: telegramID,
		Username:   username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// JWTMiddleware validates JWT tokens for protected routes
func JWTMiddleware(secret string) fiber.Handler {
	return func(c fiber.Ctx) error {
		tokenString := c.Get("Authorization")
		if tokenString == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "missing authorization token",
			})
		}

		// Remove "Bearer " prefix
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")

		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid or expired token",
			})
		}

		if claims, ok := token.Claims.(*Claims); ok {
			c.Locals("telegramID", claims.TelegramID)
			c.Locals("username", claims.Username)
		}

		return c.Next()
	}
}

func hmacSHA256(key, data string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hmacSHA256Raw(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
