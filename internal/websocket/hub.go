package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"jambo-bingo/backend/internal/database"
	"jambo-bingo/backend/internal/models"
	"jambo-bingo/backend/internal/services"
	"jambo-bingo/backend/internal/utils"

	"github.com/gofiber/contrib/v3/websocket"
)

// Client represents a connected WebSocket client
type Client struct {
	Conn       *websocket.Conn
	UserID     int
	TelegramID int64
	Username   string
	GameID     string
	Cartela    *models.UserCartela
	Send       chan []byte
}

// Room represents an active game room
type Room struct {
	GameID        string
	StakeAmount   int
	Status        models.RoomStatus
	Clients       map[int]*Client
	CalledBalls   []int
	CalledSet     map[int]bool
	Ticker        *time.Ticker
	BallDropTimer *time.Timer
	Mutex         sync.RWMutex
	StartTime     time.Time
	LobbyDuration time.Duration
}

// Hub manages all WebSocket rooms and clients
type Hub struct {
	rooms      map[string]*Room
	register   chan *Client
	unregister chan *Client
	broadcast  chan BroadcastMessage
	mutex      sync.RWMutex
	db         *database.DB
	redis      *database.RedisClient
	gameSvc    *services.GameService
	walletSvc  *services.WalletService
}

type BroadcastMessage struct {
	GameID  string
	Message interface{}
}

// WebSocketEvent represents incoming/outgoing WebSocket events
type WebSocketEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
	Token string          `json:"token,omitempty"`
	Tier  int             `json:"tier,omitempty"`
	CartelaNumber int     `json:"cartela_number,omitempty"`
}

// RoomTickerData represents lobby countdown broadcast
type RoomTickerData struct {
	GameID            string  `json:"game_id"`
	TimeRemainingS    int     `json:"time_remaining_s"`
	PlayersRegistered int     `json:"players_registered"`
	PrizePoolDerash   float64 `json:"prize_pool_derash"`
}

// BallDropData represents a called ball
type BallDropData struct {
	Ball            string   `json:"ball"`
	History         []string `json:"history"`
	TotalCalledCount int     `json:"total_called_count"`
}

// MatchResolvedData represents game resolution
type MatchResolvedData struct {
	WinnerUsername    string          `json:"winner_username"`
	WinningCartela    int             `json:"winning_cartela"`
	PrizeWon          float64         `json:"prize_won"`
	WinningMatrix     WinningMatrix   `json:"winning_matrix"`
}

type WinningMatrix struct {
	HitNumbers        []int           `json:"hit_numbers"`
	FullBoardSnapshot [5][5]int       `json:"full_board_snapshot"`
}

func NewHub(db *database.DB, redis *database.RedisClient, gameSvc *services.GameService, walletSvc *services.WalletService) *Hub {
	return &Hub{
		rooms:      make(map[string]*Room),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan BroadcastMessage),
		db:         db,
		redis:      redis,
		gameSvc:    gameSvc,
		walletSvc:  walletSvc,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.handleRegister(client)
		case client := <-h.unregister:
			h.handleUnregister(client)
		case msg := <-h.broadcast:
			h.handleBroadcast(msg)
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	h.mutex.Lock()
	room, exists := h.rooms[client.GameID]
	h.mutex.Unlock()

	if !exists {
		return
	}

	room.Mutex.Lock()
	room.Clients[client.UserID] = client
	room.Mutex.Unlock()

	// Send current room state to new client
	if room.Status == models.RoomStatusLobby {
		elapsed := time.Since(room.StartTime)
		remaining := int(room.LobbyDuration.Seconds() - elapsed.Seconds())
		if remaining < 0 {
			remaining = 0
		}

		data := RoomTickerData{
			GameID:            room.GameID,
			TimeRemainingS:    remaining,
			PlayersRegistered: len(room.Clients),
			PrizePoolDerash:   float64(len(room.Clients)*room.StakeAmount) * 0.85,
		}

		msg, _ := json.Marshal(map[string]interface{}{
			"event": "room:ticker",
			"data":  data,
		})
		client.Send <- msg
	} else if room.Status == models.RoomStatusActive {
		// Send current game state
		history := make([]string, 0, len(room.CalledBalls))
		for _, ball := range room.CalledBalls {
			history = append(history, utils.FormatBall(ball))
		}

		data := BallDropData{
			Ball:             utils.FormatBall(room.CalledBalls[len(room.CalledBalls)-1]),
			History:          history,
			TotalCalledCount: len(room.CalledBalls),
		}

		msg, _ := json.Marshal(map[string]interface{}{
			"event": "match:ball_drop",
			"data":  data,
		})
		client.Send <- msg
	}
}

func (h *Hub) handleUnregister(client *Client) {
	h.mutex.RLock()
	room, exists := h.rooms[client.GameID]
	h.mutex.RUnlock()

	if !exists {
		return
	}

	room.Mutex.Lock()
	delete(room.Clients, client.UserID)
	room.Mutex.Unlock()

	close(client.Send)
}

func (h *Hub) handleBroadcast(msg BroadcastMessage) {
	h.mutex.RLock()
	room, exists := h.rooms[msg.GameID]
	h.mutex.RUnlock()

	if !exists {
		return
	}

	room.Mutex.RLock()
	clients := make([]*Client, 0, len(room.Clients))
	for _, c := range room.Clients {
		clients = append(clients, c)
	}
	room.Mutex.RUnlock()

	data, err := json.Marshal(msg.Message)
	if err != nil {
		return
	}

	for _, client := range clients {
		select {
		case client.Send <- data:
		default:
			// Channel full, skip
		}
	}
}

// CreateRoom initializes a new game room
func (h *Hub) CreateRoom(gameID string, stakeAmount int, lobbyDuration time.Duration) *Room {
	room := &Room{
		GameID:        gameID,
		StakeAmount:   stakeAmount,
		Status:        models.RoomStatusLobby,
		Clients:       make(map[int]*Client),
		CalledBalls:   []int{},
		CalledSet:     make(map[int]bool),
		StartTime:     time.Now(),
		LobbyDuration: lobbyDuration,
	}

	h.mutex.Lock()
	h.rooms[gameID] = room
	h.mutex.Unlock()

	// Start lobby ticker
	room.Ticker = time.NewTicker(1 * time.Second)
	go h.runLobbyTicker(room)

	// Schedule game start
	go h.scheduleGameStart(room, lobbyDuration)

	return room
}

func (h *Hub) runLobbyTicker(room *Room) {
	for range room.Ticker.C {
		room.Mutex.RLock()
		if room.Status != models.RoomStatusLobby {
			room.Mutex.RUnlock()
			return
		}
		room.Mutex.RUnlock()

		elapsed := time.Since(room.StartTime)
		remaining := int(room.LobbyDuration.Seconds() - elapsed.Seconds())
		if remaining < 0 {
			remaining = 0
		}

		room.Mutex.RLock()
		playerCount := len(room.Clients)
		room.Mutex.RUnlock()

		data := RoomTickerData{
			GameID:            room.GameID,
			TimeRemainingS:    remaining,
			PlayersRegistered: playerCount,
			PrizePoolDerash:   float64(playerCount*room.StakeAmount) * 0.85,
		}

		msg := map[string]interface{}{
			"event": "room:ticker",
			"data":  data,
		}

		h.broadcast <- BroadcastMessage{
			GameID:  room.GameID,
			Message: msg,
		}

		if remaining <= 0 {
			return
		}
	}
}

func (h *Hub) scheduleGameStart(room *Room, delay time.Duration) {
	<-time.After(delay)

	room.Mutex.Lock()
	if room.Status != models.RoomStatusLobby {
		room.Mutex.Unlock()
		return
	}

	room.Status = models.RoomStatusActive
	room.Mutex.Unlock()

	// Update database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.gameSvc.UpdateGameStatus(ctx, room.GameID, models.RoomStatusActive)

	// Start ball drops
	go h.runBallDrops(room)
}

func (h *Hub) runBallDrops(room *Room) {
	availableBalls := make([]int, 75)
	for i := 0; i < 75; i++ {
		availableBalls[i] = i + 1
	}

	// Shuffle
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(availableBalls), func(i, j int) {
		availableBalls[i], availableBalls[j] = availableBalls[j], availableBalls[i]
	})

	ballIndex := 0

	for {
		if ballIndex >= len(availableBalls) {
			break
		}

		// Random delay between 3-5 seconds
		delay := time.Duration(3000+rand.Intn(2000)) * time.Millisecond
		<-time.After(delay)

		room.Mutex.Lock()
		if room.Status != models.RoomStatusActive {
			room.Mutex.Unlock()
			return
		}

		ball := availableBalls[ballIndex]
		room.CalledBalls = append(room.CalledBalls, ball)
		room.CalledSet[ball] = true
		room.Mutex.Unlock()

		ballIndex++

		// Update database
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		h.gameSvc.AddBallCalled(ctx, room.GameID, ball)
		cancel()

		// Build history
		room.Mutex.RLock()
		history := make([]string, 0)
		start := len(room.CalledBalls) - 4
		if start < 0 {
			start = 0
		}
		for i := start; i < len(room.CalledBalls)-1; i++ {
			history = append(history, utils.FormatBall(room.CalledBalls[i]))
		}
		room.Mutex.RUnlock()

		data := BallDropData{
			Ball:             utils.FormatBall(ball),
			History:          history,
			TotalCalledCount: ballIndex,
		}

		msg := map[string]interface{}{
			"event": "match:ball_drop",
			"data":  data,
		}

		h.broadcast <- BroadcastMessage{
			GameID:  room.GameID,
			Message: msg,
		}

		// Check for winners
		h.checkWinners(room)
	}
}

func (h *Hub) checkWinners(room *Room) {
	room.Mutex.RLock()
	clients := make([]*Client, 0, len(room.Clients))
	for _, c := range room.Clients {
		clients = append(clients, c)
	}
	calledSet := make(map[int]bool)
	for k, v := range room.CalledSet {
		calledSet[k] = v
	}
	room.Mutex.RUnlock()

	for _, client := range clients {
		if client.Cartela == nil {
			continue
		}

		won, winType := utils.CheckBingoWin(client.Cartela.MatrixData, calledSet)
		if won {
			h.resolveGame(room, client, winType)
			return
		}
	}
}

func (h *Hub) resolveGame(room *Room, winner *Client, winType string) {
	room.Mutex.Lock()
	if room.Status == models.RoomStatusResolution {
		room.Mutex.Unlock()
		return
	}
	room.Status = models.RoomStatusResolution
	room.Mutex.Unlock()

	// Update database
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.gameSvc.ResolveGame(ctx, room.GameID, winner.UserID, winner.Cartela.CartelaNumber)

	// Credit winner
	room.Mutex.RLock()
	playerCount := len(room.Clients)
	room.Mutex.RUnlock()

	prizePool := float64(playerCount*room.StakeAmount) * 0.85
	h.walletSvc.CreditWin(ctx, winner.UserID, prizePool, room.GameID)

	// Build hit numbers
	var hitNumbers []int
	for _, ball := range room.CalledBalls {
		hitNumbers = append(hitNumbers, ball)
	}

	data := MatchResolvedData{
		WinnerUsername: winner.Username,
		WinningCartela: winner.Cartela.CartelaNumber,
		PrizeWon:       prizePool,
		WinningMatrix: WinningMatrix{
			HitNumbers:        hitNumbers,
			FullBoardSnapshot: winner.Cartela.MatrixData,
		},
	}

	msg := map[string]interface{}{
		"event": "match:resolved",
		"data":  data,
	}

	h.broadcast <- BroadcastMessage{
		GameID:  room.GameID,
		Message: msg,
	}

	// Clean up room after delay
	go func() {
		<-time.After(5 * time.Second)
		h.mutex.Lock()
		delete(h.rooms, room.GameID)
		h.mutex.Unlock()
	}()
}

// GetOrCreateRoomForTier gets existing lobby or creates new game room
func (h *Hub) GetOrCreateRoomForTier(ctx context.Context, stakeAmount int, lobbyDuration time.Duration) (*Room, error) {
	h.mutex.RLock()
	for _, room := range h.rooms {
		if room.StakeAmount == stakeAmount && room.Status == models.RoomStatusLobby {
			h.mutex.RUnlock()
			return room, nil
		}
	}
	h.mutex.RUnlock()

	// Check database for existing lobby
	session, err := h.gameSvc.GetActiveGameByTier(ctx, stakeAmount)
	if err != nil {
		return nil, err
	}

	if session != nil && session.Status == models.RoomStatusLobby {
		// Room exists in DB but not in memory, recreate
		room := h.CreateRoom(session.GameID, stakeAmount, lobbyDuration)
		return room, nil
	}

	// Create new game session
	session, err = h.gameSvc.CreateGameSession(ctx, stakeAmount)
	if err != nil {
		return nil, err
	}

	room := h.CreateRoom(session.GameID, stakeAmount, lobbyDuration)
	return room, nil
}

// HandleWebSocket is the Fiber WebSocket handler
func (h *Hub) HandleWebSocket(c *websocket.Conn) {
	defer c.Close()

	client := &Client{
		Conn: c,
		Send: make(chan []byte, 256),
	}

	// Start write pump
	go func() {
		for msg := range client.Send {
			if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Read pump
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			if client.GameID != "" {
				h.unregister <- client
			}
			break
		}

		var event WebSocketEvent
		if err := json.Unmarshal(msg, &event); err != nil {
			continue
		}

		switch event.Event {
		case "room:join":
			h.handleRoomJoin(client, &event)
		case "card:select":
			h.handleCardSelect(client, &event)
		}
	}
}

func (h *Hub) handleRoomJoin(client *Client, event *WebSocketEvent) {
	// In production, validate token and extract user info
	// For now, simplified flow
	ctx := context.Background()

	room, err := h.GetOrCreateRoomForTier(ctx, event.Tier, 30*time.Second)
	if err != nil {
		client.Send <- []byte(`{"event":"error","data":"failed to join room"}`)
		return
	}

	client.GameID = room.GameID
	h.register <- client
}

func (h *Hub) handleCardSelect(client *Client, event *WebSocketEvent) {
	if client.GameID == "" {
		client.Send <- []byte(`{"event":"error","data":"not in a room"}`)
		return
	}

	ctx := context.Background()
	// In production, get userID from authenticated context
	userID := client.UserID
	if userID == 0 {
		userID = 1 // Placeholder
	}

	cartela, err := h.gameSvc.JoinGame(ctx, userID, client.GameID, event.CartelaNumber, h.walletSvc)
	if err != nil {
		client.Send <- []byte(fmt.Sprintf(`{"event":"error","data":"%s"}`, err.Error()))
		return
	}

	client.Cartela = cartela

	// Acknowledge selection
	ack, _ := json.Marshal(map[string]interface{}{
		"event": "card:confirmed",
		"data": map[string]interface{}{
			"cartela_number": cartela.CartelaNumber,
			"matrix":         cartela.MatrixData,
		},
	})
	client.Send <- ack
}
