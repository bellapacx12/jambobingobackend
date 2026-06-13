package handlers

import (
	"strconv"

	"jambo-bingo/backend/internal/services"

	"github.com/gofiber/fiber/v3"
)

type WalletHandler struct {
	walletService *services.WalletService
}

func NewWalletHandler(walletService *services.WalletService) *WalletHandler {
	return &WalletHandler{walletService: walletService}
}

// GetBalance returns the user's wallet balances
func (h *WalletHandler) GetBalance(c fiber.Ctx) error {
	userIDStr := c.Params("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid user_id",
		})
	}

	wallet, err := h.walletService.GetWallet(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "wallet not found",
		})
	}

	return c.JSON(fiber.Map{
		"main_balance": wallet.MainBalance,
		"play_balance": wallet.PlayBalance,
	})
}

// GetHistory returns transaction history
func (h *WalletHandler) GetHistory(c fiber.Ctx) error {
	userIDStr := c.Params("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid user_id",
		})
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	transactions, err := h.walletService.GetTransactionHistory(c.Context(), userID, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to fetch history",
		})
	}

	return c.JSON(fiber.Map{
		"transactions": transactions,
	})
}
