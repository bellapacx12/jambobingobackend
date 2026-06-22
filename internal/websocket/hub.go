package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"jambo-bingo/backend/internal/database"
	"jambo-bingo/backend/internal/models"
	"jambo-bingo/backend/internal/services"
	"jambo-bingo/backend/internal/utils"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/golang-jwt/jwt/v5"
)

// ─── Types ───────────────────────────────────────────────────────────────────

type Client struct {
	Conn       *websocket.Conn
	UserID     int64
	TelegramID int64
	Username   string
	GameID     string
	Cartela    *models.UserCartela
	Send       chan []byte
}

type Room struct {
	GameID        string
	StakeAmount   int
	Status        models.RoomStatus
	Clients       map[int64]*Client        // userID -> connection
	SelectedCards map[int64]*models.UserCartela // userID -> cartela (ready players)
	CalledBalls   []int
	CalledSet     map[int]bool
	Ticker        *time.Ticker
	Mutex         sync.RWMutex
	StartTime     time.Time
	LobbyDuration time.Duration
	MinPlayers    int
}

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
	jwtSecret  string
}

type BroadcastMessage struct {
	GameID  string
	Message interface{}
}

type WebSocketEvent struct {
	Event         string          `json:"event"`
	Data          json.RawMessage `json:"data,omitempty"`
	Token         string          `json:"token,omitempty"`
	Tier          int             `json:"tier,omitempty"`
	CartelaNumber int             `json:"cartela_number,omitempty"`
}

type RoomTickerData struct {
	GameID          string  `json:"game_id"`
	TimeRemainingS  int     `json:"time_remaining_s"`
	PlayersJoined   int     `json:"players_joined"`
	PlayersReady    int     `json:"players_ready"`
	MinRequired     int     `json:"min_required"`
	PrizePoolDerash float64 `json:"prize_pool_derash"`
}

type BallDropData struct {
	Ball             string   `json:"ball"`
	History          []string `json:"history"`
	TotalCalledCount int      `json:"total_called_count"`
}

type MatchResolvedData struct {
	WinnerUsername    string        `json:"winner_username"`
	WinningCartela    int           `json:"winning_cartela"`
	PrizeWon          float64       `json:"prize_won"`
	WinningMatrix     WinningMatrix `json:"winning_matrix"`
}

type WinningMatrix struct {
	HitNumbers        []int     `json:"hit_numbers"`
	FullBoardSnapshot [5][5]int `json:"full_board_snapshot"`
}
type Claims struct {
	TelegramID int64  `json:"telegram_id"`
	Username   string `json:"username"`
	jwt.RegisteredClaims
}
// ─── Constructor ───────────────────────────────────────────────────────────────

func NewHub(db *database.DB, redis *database.RedisClient, gameSvc *services.GameService, walletSvc *services.WalletService, jwtSecret string) *Hub {
	return &Hub{
		rooms:      make(map[string]*Room),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan BroadcastMessage),
		db:         db,
		redis:      redis,
		gameSvc:    gameSvc,
		walletSvc:  walletSvc,
		jwtSecret:  jwtSecret,
	}
}

// ─── Main Loop ─────────────────────────────────────────────────────────────────

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

// ─── Token Parser ──────────────────────────────────────────────────────────────

func (h *Hub) parseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(h.jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// ─── Register / Unregister ─────────────────────────────────────────────────────

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

	// ── Send current lobby state ─────────────────────────────────────────────
	room.Mutex.RLock()
	elapsed := time.Since(room.StartTime)
	remaining := int(room.LobbyDuration.Seconds() - elapsed.Seconds())
	if remaining < 0 {
		remaining = 0
	}
	playerCount := len(room.Clients)
	readyCount := len(room.SelectedCards)

	// Build selected cards list
	selectedCards := make([]map[string]interface{}, 0, len(room.SelectedCards))
	for uid, cartela := range room.SelectedCards {
		uname := ""
		if c, ok := room.Clients[uid]; ok {
			uname = c.Username
		}
		selectedCards = append(selectedCards, map[string]interface{}{
			"user_id":        uid,
			"username":       uname,
			"cartela_number": cartela.CartelaNumber,
			"matrix":         cartela.MatrixData,
		})
	}
	room.Mutex.RUnlock()

	// Room ticker
	tickerMsg, _ := json.Marshal(map[string]interface{}{
		"event": "room:ticker",
		"data": RoomTickerData{
			GameID:          room.GameID,
			TimeRemainingS:  remaining,
			PlayersJoined:   playerCount,
			PlayersReady:    readyCount,
			MinRequired:     room.MinPlayers,
			PrizePoolDerash: float64(readyCount*room.StakeAmount) * 0.85,
		},
	})
	client.Send <- tickerMsg
    
	log.Printf("DEBUG: Sending ticker to user %d: %s", client.UserID, string(tickerMsg))
	// Selected cards snapshot
	if len(selectedCards) > 0 {
		cardsMsg, _ := json.Marshal(map[string]interface{}{
			"event": "room:selected_cards",
			"data":  selectedCards,
		})
		client.Send <- cardsMsg
	}

	// If game already active, send current ball state
	if room.Status == models.RoomStatusActive && len(room.CalledBalls) > 0 {
		history := make([]string, 0, len(room.CalledBalls))
		for _, ball := range room.CalledBalls {
			history = append(history, utils.FormatBall(ball))
		}
		drop := BallDropData{
			Ball:             utils.FormatBall(room.CalledBalls[len(room.CalledBalls)-1]),
			History:          history,
			TotalCalledCount: len(room.CalledBalls),
		}
		activeMsg, _ := json.Marshal(map[string]interface{}{
			"event": "match:ball_drop",
			"data":  drop,
		})
		client.Send <- activeMsg
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
	  log.Printf("DEBUG: Broadcasting to room %s: %+v", msg.GameID, msg.Message)
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
		}
	}
}

// ─── Room Lifecycle ────────────────────────────────────────────────────────────

func (h *Hub) CreateRoom(gameID string, stakeAmount int, lobbyDuration time.Duration, minPlayers int) *Room {
	room := &Room{
		GameID:        gameID,
		StakeAmount:   stakeAmount,
		Status:        models.RoomStatusLobby,
		Clients:       make(map[int64]*Client),
		SelectedCards: make(map[int64]*models.UserCartela),
		CalledBalls:   []int{},
		CalledSet:     make(map[int]bool),
		StartTime:     time.Now(),
		LobbyDuration: lobbyDuration,
		MinPlayers:    minPlayers,
	}

	h.mutex.Lock()
	h.rooms[gameID] = room
	h.mutex.Unlock()

	// Start countdown broadcast
	room.Ticker = time.NewTicker(1 * time.Second)
	go h.runLobbyTicker(room)

	// Start lobby timer
	go h.scheduleGameStart(room)

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
		readyCount := len(room.SelectedCards)
		room.Mutex.RUnlock()

		data := RoomTickerData{
			GameID:          room.GameID,
			TimeRemainingS:  remaining,
			PlayersJoined:   playerCount,
			PlayersReady:    readyCount,
			MinRequired:     room.MinPlayers,
			PrizePoolDerash: float64(readyCount*room.StakeAmount) * 0.85,
		}

		h.broadcast <- BroadcastMessage{
			GameID: room.GameID,
			Message: map[string]interface{}{
				"event": "room:ticker",
				"data":  data,
			},
		}

		if remaining <= 0 {
			return
		}
	}
}

// scheduleGameStart loops until enough players are ready, then starts the game.
// If the timer expires and ready < min, it restarts automatically.
func (h *Hub) scheduleGameStart(room *Room) {
	for {
		room.Mutex.Lock()
		if room.Status != models.RoomStatusLobby {
			room.Mutex.Unlock()
			return
		}
		room.StartTime = time.Now()
		room.Mutex.Unlock()

		<-time.After(room.LobbyDuration)

		room.Mutex.Lock()
		if room.Status != models.RoomStatusLobby {
			room.Mutex.Unlock()
			return
		}

		readyCount := len(room.SelectedCards)
		if readyCount >= room.MinPlayers {
			room.Status = models.RoomStatusActive
			room.Mutex.Unlock()
			h.startGame(room)
			return
		}

		// Not enough players — restart timer automatically
		room.Mutex.Unlock()

		h.broadcast <- BroadcastMessage{
			GameID: room.GameID,
			Message: map[string]interface{}{
				"event": "room:timer_restart",
				"data": map[string]interface{}{
					"reason":        "waiting for more players",
					"players_ready": readyCount,
					"min_required":  room.MinPlayers,
				},
			},
		}
	}
}

// tryStartGame checks if we have enough players to start early.
func (h *Hub) tryStartGame(room *Room) {
	room.Mutex.Lock()
	if room.Status != models.RoomStatusLobby {
		room.Mutex.Unlock()
		return
	}

	readyCount := len(room.SelectedCards)
	if readyCount >= room.MinPlayers {
		room.Status = models.RoomStatusActive
		room.Mutex.Unlock()
		h.startGame(room)
		return
	}
	room.Mutex.Unlock()
}

// startGame deducts stakes, updates DB, and begins ball drops.
func (h *Hub) startGame(room *Room) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Collect all ready players
	room.Mutex.RLock()
	userIDs := make([]int64, 0, len(room.SelectedCards))
for uid := range room.SelectedCards {
    userIDs = append(userIDs, uid)
}
	room.Mutex.RUnlock()

	// Deduct stakes from everyone who selected a card
	if err := h.gameSvc.DeductStakesAndStart(ctx, room.GameID, userIDs, room.StakeAmount); err != nil {
		log.Printf("Failed to start game %s: %v", room.GameID, err)

		// Revert to lobby so timer can restart
		room.Mutex.Lock()
		room.Status = models.RoomStatusLobby
		room.Mutex.Unlock()

		h.broadcast <- BroadcastMessage{
			GameID: room.GameID,
			Message: map[string]interface{}{
				"event": "game:start_failed",
				"data": map[string]interface{}{
					"error": err.Error(),
				},
			},
		}
		return
	}

	room.Mutex.RLock()
	playerCount := len(room.SelectedCards)
	room.Mutex.RUnlock()

	h.broadcast <- BroadcastMessage{
		GameID: room.GameID,
		Message: map[string]interface{}{
			"event": "game:started",
			"data": map[string]interface{}{
				"game_id":    room.GameID,
				"players":    playerCount,
				"prize_pool": float64(playerCount*room.StakeAmount) * 0.85,
			},
		},
	}

	go h.runBallDrops(room)
}

// ─── Ball Drops & Resolution ───────────────────────────────────────────────────

func (h *Hub) runBallDrops(room *Room) {
	availableBalls := make([]int, 75)
	for i := 0; i < 75; i++ {
		availableBalls[i] = i + 1
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(availableBalls), func(i, j int) {
		availableBalls[i], availableBalls[j] = availableBalls[j], availableBalls[i]
	})

	ballIndex := 0

	for {
		if ballIndex >= len(availableBalls) {
			break
		}

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

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		h.gameSvc.AddBallCalled(ctx, room.GameID, ball)
		cancel()

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

		h.broadcast <- BroadcastMessage{
			GameID: room.GameID,
			Message: map[string]interface{}{
				"event": "match:ball_drop",
				"data":  data,
			},
		}

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.gameSvc.ResolveGame(ctx, room.GameID, winner.UserID, winner.Cartela.CartelaNumber)

	room.Mutex.RLock()
	playerCount := len(room.SelectedCards)
	room.Mutex.RUnlock()

	prizePool := float64(playerCount*room.StakeAmount) * 0.85
	h.walletSvc.CreditWin(ctx, winner.UserID, prizePool, room.GameID)

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

	h.broadcast <- BroadcastMessage{
		GameID: room.GameID,
		Message: map[string]interface{}{
			"event": "match:resolved",
			"data":  data,
		},
	}

	go func() {
		<-time.After(5 * time.Second)
		h.mutex.Lock()
		delete(h.rooms, room.GameID)
		h.mutex.Unlock()
	}()
}

// ─── Room Lookup ───────────────────────────────────────────────────────────────

func (h *Hub) GetOrCreateRoomForTier(ctx context.Context, stakeAmount int, lobbyDuration time.Duration, minPlayers int) (*Room, error) {
	h.mutex.RLock()
	for _, room := range h.rooms {
		if room.StakeAmount == stakeAmount && room.Status == models.RoomStatusLobby {
			h.mutex.RUnlock()
			return room, nil
		}
	}
	h.mutex.RUnlock()

	// Check DB for existing lobby
	session, err := h.gameSvc.GetActiveGameByTier(ctx, stakeAmount)
	if err != nil {
		return nil, err
	}

	if session != nil && session.Status == models.RoomStatusLobby {
		room := h.CreateRoom(session.GameID, stakeAmount, lobbyDuration, minPlayers)
		return room, nil
	}

	// Create new
	session, err = h.gameSvc.CreateGameSession(ctx, stakeAmount)
	if err != nil {
		return nil, err
	}

	room := h.CreateRoom(session.GameID, stakeAmount, lobbyDuration, minPlayers)
	return room, nil
}

// ─── WebSocket Handler ───────────────────────────────────────────────────────────

func (h *Hub) HandleWebSocket(c *websocket.Conn) {
	defer c.Close()

	client := &Client{
		Conn: c,
		Send: make(chan []byte, 256),
	}

	// Write pump
	go func() {
		for msg := range client.Send {
			if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("DEBUG: WriteMessage error for user %d: %v", client.UserID, err)
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
	if event.Token == "" {
		client.Send <- []byte(`{"event":"error","data":"missing token"}`)
		return
	}

	claims, err := h.parseToken(event.Token)
	if err != nil {
		client.Send <- []byte(`{"event":"error","data":"invalid token"}`)
		return
	}

	

	client.UserID = claims.TelegramID 
	client.Username = claims.Username
	ctx := context.Background()
	room, err := h.GetOrCreateRoomForTier(ctx, event.Tier, 30*time.Second, 2)
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
	if client.UserID == 0 {
		client.Send <- []byte(`{"event":"error","data":"not authenticated"}`)
		return
	}

	ctx := context.Background()
	cartela, err := h.gameSvc.SelectCard(ctx, client.UserID, client.GameID, event.CartelaNumber)
	if err != nil {
		client.Send <- []byte(fmt.Sprintf(`{"event":"error","data":"%s"}`, err.Error()))
		return
	}

	client.Cartela = cartela

	room, exists := h.rooms[client.GameID]
	if !exists {
		return
	}

	room.Mutex.Lock()
	room.SelectedCards[client.UserID] = cartela
	room.Mutex.Unlock()

	// Broadcast to everyone that this player selected a card
	cardData := map[string]interface{}{
		"user_id":        client.UserID,
		"username":       client.Username,
		"cartela_number": cartela.CartelaNumber,
		"matrix":         cartela.MatrixData,
	}
	h.broadcast <- BroadcastMessage{
		GameID: client.GameID,
		Message: map[string]interface{}{
			"event": "card:selected",
			"data":  cardData,
		},
	}

	// Confirm to the sender
	ack, _ := json.Marshal(map[string]interface{}{
		"event": "card:confirmed",
		"data":  cardData,
	})
	client.Send <- ack

	// Try to start early if we have enough players
	h.tryStartGame(room)
}