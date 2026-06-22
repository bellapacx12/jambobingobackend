package services

import (
	"context"
	"fmt"

	"jambo-bingo/backend/internal/database"
	"jambo-bingo/backend/internal/models"
)

type WalletService struct {
	db *database.DB
}

func NewWalletService(db *database.DB) *WalletService {
	return &WalletService{db: db}
}

// GetWallet retrieves a user's wallet
func (s *WalletService) GetWallet(ctx context.Context, userID int64) (*models.Wallet, error) {
	var wallet models.Wallet
	err := s.db.Pool.QueryRow(ctx, `
		SELECT user_id, main_balance, play_balance, updated_at
		FROM wallets WHERE user_id = $1
	`, userID).Scan(
		&wallet.UserID, &wallet.MainBalance, &wallet.PlayBalance, &wallet.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

// DeductStake deducts stake from wallet with atomic locking
func (s *WalletService) DeductStake(ctx context.Context, userID int, amount float64, gameID string) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock wallet row for update
	var mainBalance, playBalance float64
	err = tx.QueryRow(ctx, `
		SELECT main_balance, play_balance FROM wallets WHERE user_id = $1 FOR UPDATE
	`, userID).Scan(&mainBalance, &playBalance)
	if err != nil {
		return fmt.Errorf("failed to lock wallet: %w", err)
	}

	totalAvailable := mainBalance + playBalance
	if totalAvailable < amount {
		return fmt.Errorf("insufficient balance: have %.2f, need %.2f", totalAvailable, amount)
	}

	// Deduct from play_balance first, then main_balance
	newMain := mainBalance
	newPlay := playBalance

	if playBalance >= amount {
		newPlay = playBalance - amount
	} else {
		remaining := amount - playBalance
		newPlay = 0
		newMain = mainBalance - remaining
	}

	_, err = tx.Exec(ctx, `
		UPDATE wallets SET main_balance = $1, play_balance = $2, updated_at = NOW()
		WHERE user_id = $3
	`, newMain, newPlay, userID)
	if err != nil {
		return fmt.Errorf("failed to update wallet: %w", err)
	}

	// Record transaction
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (user_id, type, amount, balance_after, game_id, description)
		VALUES ($1, 'stake', $2, $3, $4, $5)
	`, userID, -amount, newMain+newPlay, gameID,
		fmt.Sprintf("Stake deduction for game %s", gameID))
	if err != nil {
		return fmt.Errorf("failed to record transaction: %w", err)
	}

	return tx.Commit(ctx)
}

// CreditWin credits winnings to user's main balance
func (s *WalletService) CreditWin(ctx context.Context, userID int64, amount float64, gameID string) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var mainBalance float64
	err = tx.QueryRow(ctx, `
		SELECT main_balance FROM wallets WHERE user_id = $1 FOR UPDATE
	`, userID).Scan(&mainBalance)
	if err != nil {
		return fmt.Errorf("failed to lock wallet: %w", err)
	}

	newBalance := mainBalance + amount
	_, err = tx.Exec(ctx, `
		UPDATE wallets SET main_balance = $1, updated_at = NOW() WHERE user_id = $2
	`, newBalance, userID)
	if err != nil {
		return fmt.Errorf("failed to update wallet: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (user_id, type, amount, balance_after, game_id, description)
		VALUES ($1, 'win', $2, $3, $4, $5)
	`, userID, amount, newBalance, gameID,
		fmt.Sprintf("Winning payout for game %s", gameID))
	if err != nil {
		return fmt.Errorf("failed to record transaction: %w", err)
	}

	return tx.Commit(ctx)
}

// GetTransactionHistory retrieves user's transaction history
func (s *WalletService) GetTransactionHistory(ctx context.Context, userID int, limit int) ([]models.Transaction, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, type, amount, balance_after, game_id, description, created_at
		FROM transactions WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transactions []models.Transaction
	for rows.Next() {
		var t models.Transaction
		var gameID *string
		err := rows.Scan(&t.ID, &t.UserID, &t.Type, &t.Amount, &t.BalanceAfter, &gameID, &t.Description, &t.CreatedAt)
		if err != nil {
			return nil, err
		}
		if gameID != nil {
			t.GameID = gameID
		}
		transactions = append(transactions, t)
	}

	return transactions, rows.Err()
}
