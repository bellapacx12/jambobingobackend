package models

import (
	"encoding/json"
	"time"
)

// RoomStatus represents the current state of a game room
type RoomStatus string

const (
	RoomStatusLobby      RoomStatus = "lobby"
	RoomStatusActive     RoomStatus = "active"
	RoomStatusResolution RoomStatus = "resolution"
)

// MatchOutcome represents the result for a player's cartela
type MatchOutcome string

const (
	OutcomeWon       MatchOutcome = "won"
	OutcomeLost      MatchOutcome = "lost"
	OutcomeSpectated MatchOutcome = "spectated"
)

// User represents a registered player
type User struct {
	ID           int       `json:"id" db:"id"`
	TelegramID   int64     `json:"telegram_id" db:"telegram_id"`
	Username     string    `json:"username" db:"username"`
	PhoneNumber  string    `json:"phone_number" db:"phone_number"`
	IsVerified   bool      `json:"is_verified" db:"is_verified"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// Wallet represents a user's financial accounts
type Wallet struct {
	UserID       int       `json:"user_id" db:"user_id"`
	MainBalance  float64   `json:"main_balance" db:"main_balance"`
	PlayBalance  float64   `json:"play_balance" db:"play_balance"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// GameSession represents an active or completed bingo game
type GameSession struct {
	GameID         string     `json:"game_id" db:"game_id"`
	StakeAmount    int        `json:"stake_amount" db:"stake_amount"`
	TotalPlayers   int        `json:"total_players" db:"total_players"`
	PrizePool      float64    `json:"prize_pool" db:"prize_pool"`
	WinningCartela *int       `json:"winning_cartela,omitempty" db:"winning_cartela"`
	WinnerID       *int       `json:"winner_id,omitempty" db:"winner_id"`
	Status         RoomStatus `json:"status" db:"status"`
	BallsCalled    []int      `json:"balls_called" db:"balls_called"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// UserCartela represents a player's selected board for a game
type UserCartela struct {
	ID             int           `json:"id" db:"id"`
	UserID         int64           `json:"user_id" db:"user_id"`
	GameID         string        `json:"game_id" db:"game_id"`
	CartelaNumber  int           `json:"cartela_number" db:"cartela_number"`
	MatrixData     Matrix5x5     `json:"matrix_data" db:"matrix_data"`
	Outcome        MatchOutcome  `json:"outcome" db:"outcome"`
}

// Matrix5x5 represents the bingo card grid
type Matrix5x5 [5][5]int

func (m Matrix5x5) Value() (interface{}, error) {
	return json.Marshal(m)
}

func (m *Matrix5x5) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, m)
}

// Transaction represents a financial ledger entry
type Transaction struct {
	ID           int       `json:"id" db:"id"`
	UserID       int       `json:"user_id" db:"user_id"`
	Type         string    `json:"type" db:"type"`
	Amount       float64   `json:"amount" db:"amount"`
	BalanceAfter float64   `json:"balance_after" db:"balance_after"`
	GameID       *string   `json:"game_id,omitempty" db:"game_id"`
	Description  string    `json:"description" db:"description"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// GameHistory represents a completed game for user history view
type GameHistory struct {
	GameID       string    `json:"game_id"`
	Timestamp    time.Time `json:"timestamp"`
	Stake        int       `json:"stake"`
	Cards        int       `json:"cards"`
	Prize        float64   `json:"prize"`
	Winners      int       `json:"winners"`
	Outcome      string    `json:"outcome"`
	CartelaNumber int      `json:"cartela_number"`
}

// PlayerStats represents aggregated player metrics
type PlayerStats struct {
	MainWallet    float64 `json:"main_wallet"`
	PlayWallet    float64 `json:"play_wallet"`
	GamesWon      int     `json:"games_won"`
	TotalInvites  int     `json:"total_invites"`
	TotalEarning  float64 `json:"total_earning"`
}
