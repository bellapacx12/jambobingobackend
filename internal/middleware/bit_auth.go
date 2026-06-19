package middleware

import "github.com/gofiber/fiber/v3"

func BotAuthMiddleware(botToken string) fiber.Handler {
	return func(c fiber.Ctx) error {
		token := c.Get("X-Bot-Service-Token")
		if token == "" {
			return c.Status(401).JSON(fiber.Map{"error": "missing bot service token"})
		}
		if token != botToken {
			return c.Status(403).JSON(fiber.Map{"error": "invalid bot service token"})
		}
		return c.Next()
	}
}