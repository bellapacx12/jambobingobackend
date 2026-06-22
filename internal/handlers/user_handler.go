package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"jambo-bingo/backend/internal/middleware"
	"jambo-bingo/backend/internal/services"

	"github.com/gofiber/fiber/v3"
)

type UserHandler struct {
	userService *services.UserService
}

func NewUserHandler(userService *services.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// RegisterRequest represents the contact share payload
type RegisterRequest struct {
	TelegramID   int64  `json:"telegram_id"`
	Username     string `json:"username"`
	PhoneNumber  string `json:"phone_number"`
}

// Register handles user registration with phone number
func (h *UserHandler) Register(c fiber.Ctx) error {
	var req RegisterRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.TelegramID == 0 || req.PhoneNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "telegram_id and phone_number are required",
		})
	}

	user, err := h.userService.GetOrCreateUser(c.Context(), req.TelegramID, req.Username, req.PhoneNumber)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("failed to register user: %v", err),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "🎉 ✅ Player registered successfully!",
		"user":    user,
	})
}

// GetProfile returns the authenticated user's profile
func (h *UserHandler) GetProfile(c fiber.Ctx) error {
	telegramID := c.Locals("telegramID").(int64)

	user, err := h.userService.GetUserByTelegramID(c.Context(), telegramID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "user not found",
		})
	}

	return c.JSON(fiber.Map{
		"user": user,
	})
}

// VerifyInitData validates Telegram initData and returns JWT
func (h *UserHandler) VerifyInitData(c fiber.Ctx) error {
	initData := c.Locals("initData")
	userDataRaw := c.Locals("userData")
    
	// Check nil FIRST
	if initData == nil || userDataRaw == nil {
		log.Printf("DEBUG: initData is nil: %v, userDataRaw is nil: %v", initData == nil, userDataRaw == nil)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "invalid init data",
		})
	}

	// NOW safe to type-assert and log
	initDataStr, ok := initData.(string)
	if ok && len(initDataStr) > 0 {
		end := 50
		if len(initDataStr) < 50 {
			end = len(initDataStr)
		}
		log.Printf("DEBUG: initData length: %d, start: %s", len(initDataStr), initDataStr[:end])
	}
	if initData == nil || userDataRaw == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "invalid init data",
		})
	}

	var userData struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}

	if err := json.Unmarshal([]byte(userDataRaw.(string)), &userData); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid user data",
		})
	}

	// Get or create user
	user, err := h.userService.GetOrCreateUser(c.Context(), userData.ID, userData.Username, "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to process user",
		})
	}

	// Generate JWT
	// Note: In production, pass the actual secret from config
	token, err := middleware.GenerateJWT(userData.ID, userData.Username, "change-me-in-production")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to generate token",
		})
	}

	return c.JSON(fiber.Map{
		"token":    token,
		"user":     user,
		"telegram": userData,
	})
}
func (h *UserHandler) GetUserByTelegramID(c fiber.Ctx) error {
	telegramIDStr := c.Params("telegram_id")
	telegramID, err := strconv.ParseInt(telegramIDStr, 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid telegram_id",
		})
	}

	user, err := h.userService.GetUserByTelegramID(c.Context(), telegramID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "user not found",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"user":    user,
	})
}