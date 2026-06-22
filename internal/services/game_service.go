package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"jambo-bingo/backend/internal/database"
	"jambo-bingo/backend/internal/models"
	"jambo-bingo/backend/internal/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type GameService struct {
	db    *database.DB
	redis *database.RedisClient
}

func NewGameService(db *database.DB, redis *database.RedisClient) *GameService {
	return &GameService{db: db, redis: redis}
}

// CreateGameSession initializes a new game lobby
func (s *GameService) CreateGameSession(ctx context.Context, stakeAmount int) (*models.GameSession, error) {
	gameID := utils.GenerateGameID()

	// Initial prize pool is 0, will be calculated when players join
	session := &models.GameSession{
		GameID:      gameID,
		StakeAmount: stakeAmount,
		Status:      models.RoomStatusLobby,
		PrizePool:   0,
		BallsCalled: []int{},
		CreatedAt:   time.Now(),
	}

	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO game_sessions (game_id, stake_amount, total_players, prize_pool, status, balls_called)
		VALUES ($1, $2, 0, 0.00, $3, $4)
	`, session.GameID, session.StakeAmount, session.Status, pgtype.FlatArray[int](session.BallsCalled))
	if err != nil {
		return nil, fmt.Errorf("failed to create game session: %w", err)
	}

	return session, nil
}

// GetActiveGameByTier retrieves the active/lobby game for a specific stake tier
func (s *GameService) GetActiveGameByTier(ctx context.Context, stakeAmount int) (*models.GameSession, error) {
	var session models.GameSession
	var ballsCalled pgtype.Array[int]

	err := s.db.Pool.QueryRow(ctx, `
		SELECT game_id, stake_amount, total_players, prize_pool, winning_cartela, winner_id, status, balls_called, created_at
		FROM game_sessions
		WHERE stake_amount = $1 AND status IN ('lobby', 'active')
		ORDER BY created_at DESC LIMIT 1
	`, stakeAmount).Scan(
		&session.GameID, &session.StakeAmount, &session.TotalPlayers, &session.PrizePool,
		&session.WinningCartela, &session.WinnerID, &session.Status, &ballsCalled, &session.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if ballsCalled.Elements != nil {
		session.BallsCalled = ballsCalled.Elements
	}

	return &session, nil
}

// JoinGame adds a player to a game session and deducts stake
func (s *GameService) JoinGame(ctx context.Context, userID int, gameID string, cartelaNumber int, ws *WalletService) (*models.UserCartela, error) {
	// Generate cartela matrix
	matrix := utils.GenerateCartelaMatrix()
	matrixJSON, err := json.Marshal(matrix)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal matrix: %w", err)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get game stake amount
	var stakeAmount int
	var status models.RoomStatus
	err = tx.QueryRow(ctx, `
		SELECT stake_amount, status FROM game_sessions WHERE game_id = $1 FOR UPDATE
	`, gameID).Scan(&stakeAmount, &status)
	if err != nil {
		return nil, fmt.Errorf("failed to get game: %w", err)
	}

	if status != models.RoomStatusLobby {
		return nil, fmt.Errorf("game is no longer accepting entries")
	}

	// Check if user already has a cartela for this game
	var existingID int
	err = tx.QueryRow(ctx, `
		SELECT id FROM user_cartelas WHERE user_id = $1 AND game_id = $2
	`, userID, gameID).Scan(&existingID)
	if err == nil {
		return nil, fmt.Errorf("user already has a cartela in this game")
	}

	// Deduct stake from wallet (using separate service to avoid circular deps in real app)
	// For simplicity, we do it inline here but in production use the wallet service
	var mainBalance, playBalance float64
	err = tx.QueryRow(ctx, `
		SELECT main_balance, play_balance FROM wallets WHERE user_id = $1 FOR UPDATE
	`, userID).Scan(&mainBalance, &playBalance)
	if err != nil {
		return nil, fmt.Errorf("failed to lock wallet: %w", err)
	}

	stakeFloat := float64(stakeAmount)
	totalAvailable := mainBalance + playBalance
	if totalAvailable < stakeFloat {
		return nil, fmt.Errorf("insufficient balance")
	}

	newMain := mainBalance
	newPlay := playBalance
	if playBalance >= stakeFloat {
		newPlay = playBalance - stakeFloat
	} else {
		remaining := stakeFloat - playBalance
		newPlay = 0
		newMain = mainBalance - remaining
	}

	_, err = tx.Exec(ctx, `
		UPDATE wallets SET main_balance = $1, play_balance = $2, updated_at = NOW() WHERE user_id = $3
	`, newMain, newPlay, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to update wallet: %w", err)
	}

	// Record transaction
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (user_id, type, amount, balance_after, game_id, description)
		VALUES ($1, 'stake', $2, $3, $4, $5)
	`, userID, -stakeFloat, newMain+newPlay, gameID, fmt.Sprintf("Stake for game %s", gameID))
	if err != nil {
		return nil, fmt.Errorf("failed to record transaction: %w", err)
	}

	// Create user cartela
	var cartela models.UserCartela
	err = tx.QueryRow(ctx, `
		INSERT INTO user_cartelas (user_id, game_id, cartela_number, matrix_data, outcome)
		VALUES ($1, $2, $3, $4, 'lost')
		RETURNING id, user_id, game_id, cartela_number, matrix_data, outcome
	`, userID, gameID, cartelaNumber, matrixJSON).Scan(
		&cartela.ID, &cartela.UserID, &cartela.GameID, &cartela.CartelaNumber, &cartela.MatrixData, &cartela.Outcome,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cartela: %w", err)
	}

	// Update game player count and prize pool
	_, err = tx.Exec(ctx, `
		UPDATE game_sessions 
		SET total_players = total_players + 1,
		    prize_pool = (total_players + 1) * stake_amount * 0.85
		WHERE game_id = $1
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to update game session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	return &cartela, nil
}

// GetUserCartelas retrieves all cartelas for a user in a game
func (s *GameService) GetUserCartelas(ctx context.Context, userID int, gameID string) ([]models.UserCartela, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, game_id, cartela_number, matrix_data, outcome
		FROM user_cartelas WHERE user_id = $1 AND game_id = $2
	`, userID, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cartelas []models.UserCartela
	for rows.Next() {
		var c models.UserCartela
		err := rows.Scan(&c.ID, &c.UserID, &c.GameID, &c.CartelaNumber, &c.MatrixData, &c.Outcome)
		if err != nil {
			return nil, err
		}
		cartelas = append(cartelas, c)
	}

	return cartelas, rows.Err()
}

// UpdateGameStatus updates the status of a game session
func (s *GameService) UpdateGameStatus(ctx context.Context, gameID string, status models.RoomStatus) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE game_sessions SET status = $1 WHERE game_id = $2
	`, status, gameID)
	return err
}

// AddBallCalled appends a new ball to the called balls array
func (s *GameService) AddBallCalled(ctx context.Context, gameID string, ball int) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE game_sessions SET balls_called = array_append(balls_called, $1) WHERE game_id = $2
	`, ball, gameID)
	return err
}

// ResolveGame marks a game as resolved with a winner
func (s *GameService) ResolveGame(ctx context.Context, gameID string, winnerID int, winningCartela int) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE game_sessions 
		SET status = 'resolution', winner_id = $1, winning_cartela = $2
		WHERE game_id = $3
	`, winnerID, winningCartela, gameID)
	return err
}

// GetGameHistory retrieves completed games for a user
func (s *GameService) GetGameHistory(ctx context.Context, userID int) ([]models.GameHistory, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT 
			gs.game_id,
			gs.created_at,
			gs.stake_amount,
			uc.cartela_number,
			gs.prize_pool,
			CASE WHEN gs.winner_id IS NOT NULL THEN 1 ELSE 0 END as winners,
			uc.outcome
		FROM game_sessions gs
		JOIN user_cartelas uc ON gs.game_id = uc.game_id
		WHERE uc.user_id = $1 AND gs.status = 'resolution'
		ORDER BY gs.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.GameHistory
	for rows.Next() {
		var h models.GameHistory
		var outcome models.MatchOutcome
		err := rows.Scan(&h.GameID, &h.Timestamp, &h.Stake, &h.CartelaNumber, &h.Prize, &h.Winners, &outcome)
		if err != nil {
			return nil, err
		}
		h.Cards = 1 // Each entry is one card
		h.Outcome = string(outcome)
		history = append(history, h)
	}

	return history, rows.Err()
}

// GetPlayerStats retrieves aggregated stats for a user
func (s *GameService) GetPlayerStats(ctx context.Context, userID int) (*models.PlayerStats, error) {
	var stats models.PlayerStats

	// Get wallet balances
	err := s.db.Pool.QueryRow(ctx, `
		SELECT main_balance, play_balance FROM wallets WHERE user_id = $1
	`, userID).Scan(&stats.MainWallet, &stats.PlayWallet)
	if err != nil {
		return nil, err
	}

	// Count games won
	err = s.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM user_cartelas WHERE user_id = $1 AND outcome = 'won'
	`, userID).Scan(&stats.GamesWon)
	if err != nil {
		return nil, err
	}

	// Total earnings from wins
	err = s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM transactions 
		WHERE user_id = $1 AND type = 'win'
	`, userID).Scan(&stats.TotalEarning)
	if err != nil {
		return nil, err
	}

	// Total invites (placeholder - would track referrals)
	stats.TotalInvites = 0

	return &stats, nil
}
// SelectCard creates a cartela for a user WITHOUT deducting stake.
// Stake is deducted later when the game actually starts.
func (s *GameService) SelectCard(ctx context.Context, userID int, gameID string, cartelaNumber int) (*models.UserCartela, error) {
	matrix := utils.GenerateCartelaMatrix()
	matrixJSON, err := json.Marshal(matrix)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal matrix: %w", err)
	}

	var status string
	err = s.db.Pool.QueryRow(ctx, `
		SELECT status FROM game_sessions WHERE game_id = $1
	`, gameID).Scan(&status)
	if err != nil {
		return nil, fmt.Errorf("failed to get game: %w", err)
	}
	if status != string(models.RoomStatusLobby) {
		return nil, fmt.Errorf("game is no longer accepting entries")
	}

	var existingID int
	err = s.db.Pool.QueryRow(ctx, `
		SELECT id FROM user_cartelas WHERE user_id = $1 AND game_id = $2
	`, userID, gameID).Scan(&existingID)
	if err == nil {
		return nil, fmt.Errorf("user already has a cartela in this game")
	}

	var cartela models.UserCartela
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO user_cartelas (user_id, game_id, cartela_number, matrix_data, outcome)
		VALUES ($1, $2, $3, $4, 'lost')
		RETURNING id, user_id, game_id, cartela_number, matrix_data, outcome
	`, userID, gameID, cartelaNumber, matrixJSON).Scan(
		&cartela.ID, &cartela.UserID, &cartela.GameID, &cartela.CartelaNumber, &cartela.MatrixData, &cartela.Outcome,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cartela: %w", err)
	}

	return &cartela, nil
}

// DeductStakesAndStart deducts the stake from every ready player's wallet,
// records transactions, and updates the game session prize pool.
func (s *GameService) DeductStakesAndStart(ctx context.Context, gameID string, userIDs []int, stakeAmount int) error {
	if len(userIDs) == 0 {
		return fmt.Errorf("no players to start game")
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	prizePool := float64(len(userIDs) * stakeAmount) * 0.85

	for _, userID := range userIDs {
		var mainBalance, playBalance float64
		err = tx.QueryRow(ctx, `
			SELECT main_balance, play_balance FROM wallets WHERE user_id = $1 FOR UPDATE
		`, userID).Scan(&mainBalance, &playBalance)
		if err != nil {
			return fmt.Errorf("failed to get wallet for user %d: %w", userID, err)
		}

		stakeFloat := float64(stakeAmount)
		totalAvailable := mainBalance + playBalance
		if totalAvailable < stakeFloat {
			return fmt.Errorf("insufficient balance for user %d", userID)
		}

		newMain := mainBalance
		newPlay := playBalance
		if playBalance >= stakeFloat {
			newPlay = playBalance - stakeFloat
		} else {
			remaining := stakeFloat - playBalance
			newPlay = 0
			newMain = mainBalance - remaining
		}

		_, err = tx.Exec(ctx, `
			UPDATE wallets SET main_balance = $1, play_balance = $2, updated_at = NOW() WHERE user_id = $3
		`, newMain, newPlay, userID)
		if err != nil {
			return fmt.Errorf("failed to update wallet for user %d: %w", userID, err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (user_id, type, amount, balance_after, game_id, description)
			VALUES ($1, 'stake', $2, $3, $4, $5)
		`, userID, -stakeFloat, newMain+newPlay, gameID, fmt.Sprintf("Stake for game %s", gameID))
		if err != nil {
			return fmt.Errorf("failed to record transaction for user %d: %w", userID, err)
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE game_sessions 
		SET total_players = $1, prize_pool = $2, status = 'active'
		WHERE game_id = $3
	`, len(userIDs), prizePool, gameID)
	if err != nil {
		return fmt.Errorf("failed to update game session: %w", err)
	}

	return tx.Commit(ctx)
}