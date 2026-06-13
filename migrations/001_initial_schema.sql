-- Enums for state tracking
CREATE TYPE IF NOT EXISTS room_status AS ENUM ('lobby', 'active', 'resolution');
CREATE TYPE IF NOT EXISTS match_outcome AS ENUM ('won', 'lost', 'spectated');

-- Users & Authentication Profiles
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    telegram_id BIGINT UNIQUE NOT NULL,
    username VARCHAR(100),
    phone_number VARCHAR(20) UNIQUE NOT NULL,
    is_verified BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Wallets Layer (Isolation of Promotional vs Real Capital)
CREATE TABLE IF NOT EXISTS wallets (
    user_id INT REFERENCES users(id) ON DELETE CASCADE PRIMARY KEY,
    main_balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00 CHECK (main_balance >= 0.00),
    play_balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00 CHECK (play_balance >= 0.00),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Real-Time Match Tracking Ledger
CREATE TABLE IF NOT EXISTS game_sessions (
    game_id VARCHAR(12) PRIMARY KEY,
    stake_amount INT NOT NULL,
    total_players INT NOT NULL DEFAULT 0,
    prize_pool NUMERIC(12, 2) NOT NULL,
    winning_cartela INT NULL,
    winner_id INT REFERENCES users(id) NULL,
    status room_status NOT NULL DEFAULT 'lobby',
    balls_called INT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- User Cartelas (Allocated Game Boards)
CREATE TABLE IF NOT EXISTS user_cartelas (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    game_id VARCHAR(12) REFERENCES game_sessions(game_id) ON DELETE CASCADE,
    cartela_number INT NOT NULL CHECK (cartela_number BETWEEN 1 AND 96),
    matrix_data JSONB NOT NULL,
    outcome match_outcome NOT NULL DEFAULT 'lost',
    CONSTRAINT unique_user_game UNIQUE(user_id, game_id)
);

-- Transaction Ledger for Audit Trail
CREATE TABLE IF NOT EXISTS transactions (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL, -- 'stake', 'win', 'deposit', 'withdraw', 'bonus', 'transfer'
    amount NUMERIC(12, 2) NOT NULL,
    balance_after NUMERIC(12, 2) NOT NULL,
    game_id VARCHAR(12) REFERENCES game_sessions(game_id) NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index optimizations for active queries
CREATE INDEX IF NOT EXISTS idx_game_sessions_status ON game_sessions(status);
CREATE INDEX IF NOT EXISTS idx_user_cartelas_lookup ON user_cartelas(game_id, user_id);
CREATE INDEX IF NOT EXISTS idx_transactions_user ON transactions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_users_telegram ON users(telegram_id);
