package services

import (
	"context"
	"fmt"

	"beteseb-bingo/backend/internal/database"
	"beteseb-bingo/backend/internal/models"

	"github.com/jackc/pgx/v5"
)

type UserService struct {
	db *database.DB
}

func NewUserService(db *database.DB) *UserService {
	return &UserService{db: db}
}

// GetOrCreateUser retrieves a user by Telegram ID or creates a new one
func (s *UserService) GetOrCreateUser(ctx context.Context, telegramID int64, username, phoneNumber string) (*models.User, error) {
	var user models.User
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, telegram_id, username, phone_number, is_verified, created_at
		FROM users WHERE telegram_id = $1
	`, telegramID).Scan(
		&user.ID, &user.TelegramID, &user.Username, &user.PhoneNumber, &user.IsVerified, &user.CreatedAt,
	)

	if err == nil {
		return &user, nil
	}

	if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("database error: %w", err)
	}

	// Create new user with wallet
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO users (telegram_id, username, phone_number, is_verified)
		VALUES ($1, $2, $3, true)
		RETURNING id, telegram_id, username, phone_number, is_verified, created_at
	`, telegramID, username, phoneNumber).Scan(
		&user.ID, &user.TelegramID, &user.Username, &user.PhoneNumber, &user.IsVerified, &user.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Create wallet
	_, err = tx.Exec(ctx, `
		INSERT INTO wallets (user_id, main_balance, play_balance)
		VALUES ($1, 0.00, 0.00)
	`, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &user, nil
}

// GetUserByTelegramID retrieves a user by their Telegram ID
func (s *UserService) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	var user models.User
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, telegram_id, username, phone_number, is_verified, created_at
		FROM users WHERE telegram_id = $1
	`, telegramID).Scan(
		&user.ID, &user.TelegramID, &user.Username, &user.PhoneNumber, &user.IsVerified, &user.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdatePhoneNumber updates user's phone number
func (s *UserService) UpdatePhoneNumber(ctx context.Context, telegramID int64, phoneNumber string) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE users SET phone_number = $1, is_verified = true WHERE telegram_id = $2
	`, phoneNumber, telegramID)
	return err
}
