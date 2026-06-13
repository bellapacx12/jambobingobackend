package handlers

import (
	"strconv"

	"jambo-bingo/backend/internal/services"

	"github.com/gofiber/fiber/v3"
)

type GameHandler struct {
	gameService *services.GameService
}

func NewGameHandler(gameService *services.GameService) *GameHandler {
	return &GameHandler{gameService: gameService}
}

// GetHistory returns game history for a user
func (h *GameHandler) GetHistory(c fiber.Ctx) error {
	userIDStr := c.Params("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid user_id",
		})
	}

	history, err := h.gameService.GetGameHistory(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch game history",
		})
	}

	return c.JSON(fiber.Map{
		"history": history,
	})
}

// GetStats returns player statistics
func (h *GameHandler) GetStats(c fiber.Ctx) error {
	userIDStr := c.Params("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid user_id",
		})
	}

	stats, err := h.gameService.GetPlayerStats(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch stats",
		})
	}

	return c.JSON(fiber.Map{
		"stats": stats,
	})
}

// GetActiveGame returns the current active game for a tier
func (h *GameHandler) GetActiveGame(c fiber.Ctx) error {
	tierStr := c.Query("tier")
	if tierStr == "" {
		tierStr = "10"
	}

	stakeAmount, err := strconv.Atoi(tierStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid tier",
		})
	}

	session, err := h.gameService.GetActiveGameByTier(c.Context(), stakeAmount)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch active game",
		})
	}

	if session == nil {
		return c.JSON(fiber.Map{
			"game": nil,
		})
	}

	return c.JSON(fiber.Map{
		"game": session,
	})
}
