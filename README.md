# Beteseb Bingo - Backend Core Engine

High-concurrency, low-latency real-time multiplayer Bingo backend built with Go.

## Architecture

- **Framework**: Fiber v3 (Express-inspired, built on fasthttp)
- **Database**: PostgreSQL 16+ with pgx connection pooling
- **Cache**: Redis 7+ for Pub/Sub room synchronization
- **Real-Time**: WebSocket hub with goroutine-per-room architecture
- **Security**: HMAC-SHA256 Telegram initData validation, JWT auth

## Quick Start

### Prerequisites
- Go 1.22+
- Docker & Docker Compose
- PostgreSQL 16+
- Redis 7+

### Local Development

1. **Environment Setup**
   ```bash
   cp .env.example .env
   # Edit .env with your Telegram Bot Token
   ```

2. **Start Dependencies**
   ```bash
   docker-compose up -d postgres redis
   ```

3. **Run Migrations**
   ```bash
   psql -U postgres -d beteseb_bingo -f migrations/001_initial_schema.sql
   ```

4. **Run Server**
   ```bash
   go run cmd/server/main.go
   ```

### Docker Deployment

```bash
# Full stack
docker-compose up --build

# Backend only
docker-compose up -d backend
```

## API Endpoints

### Authentication
- `POST /api/v1/auth/register` - Register with phone number
- `POST /api/v1/auth/verify` - Verify Telegram initData, returns JWT

### User
- `GET /api/v1/users/profile` - Get user profile (JWT required)

### Wallet
- `GET /api/v1/wallets/:user_id/balance` - Get wallet balances
- `GET /api/v1/wallets/:user_id/history` - Get transaction history

### Game
- `GET /api/v1/games/active?tier=10` - Get active game for tier
- `GET /api/v1/games/:user_id/history` - Get game history
- `GET /api/v1/games/:user_id/stats` - Get player stats

### WebSocket
- `ws://localhost:8080/ws` - Real-time game connection

## WebSocket Events

### Client → Server
- `room:join` - Join a game room
- `card:select` - Select cartela number

### Server → Client
- `room:ticker` - Lobby countdown updates
- `match:ball_drop` - New ball called
- `match:resolved` - Game winner declared

## Security

- HMAC-SHA256 validation of Telegram initData
- Server-side bingo card simulation (client is untrusted)
- Atomic wallet deductions with `FOR UPDATE` row locking
- JWT token expiration (24h)

## License

MIT
