package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Constants for canvas and paddle dimensions
const (
	CanvasWidth  = 800
	CanvasHeight = 600
	PaddleHeight = 100
	PaddleWidth  = 20
	MaxPaddleY   = CanvasHeight - PaddleHeight
)

// Message types
const (
	AssignMessage = "assign"
	MoveMessage   = "move"
	UpdateMessage = "update"
	GameOverMsg   = "gameover"
	ErrorMessage  = "error"
)

// Message structure
type Message struct {
	Type   string  `json:"type"`
	Player string  `json:"player,omitempty"`
	Y      *int    `json:"y,omitempty"`
	LeftY  int     `json:"leftY,omitempty"`
	RightY int     `json:"rightY,omitempty"`
	BallX  float64 `json:"ballX,omitempty"`
	BallY  float64 `json:"ballY,omitempty"`
	Winner string  `json:"winner,omitempty"` // For game over messages
}

// Ball structure representing the ball's state
type Ball struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	Vx float64 `json:"vx"`
	Vy float64 `json:"vy"`
}

// Define the upgrader
var upgrader = websocket.Upgrader{
	// Allow all origins for simplicity. In production, restrict this.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Paddle positions
type PaddlePositions struct {
	LeftY  int `json:"leftY"`
	RightY int `json:"rightY"`
}

// Game state structure
type GameState struct {
	sync.Mutex
	PanYLeft  int
	PanYRight int
	Ball      Ball
}

// Initialize game state
var gameState = GameState{
	PanYLeft:  CanvasHeight/2 - PaddleHeight/2, // 250
	PanYRight: CanvasHeight/2 - PaddleHeight/2, // 250
	Ball: Ball{
		X:  float64(CanvasWidth / 2),
		Y:  float64(CanvasHeight / 2),
		Vx: 4.0, // Horizontal velocity
		Vy: 4.0, // Vertical velocity
	},
}

var clients = make(map[*websocket.Conn]string)
var clientsMutex = sync.Mutex{}

// Tick rate (60 FPS)
var ticker = time.NewTicker(time.Millisecond * 16) // Approximately 60 FPS

// Broadcast function to send game state to all clients
func broadcastGameState() {
	gameState.Lock()
	defer gameState.Unlock()

	msg := Message{
		Type:   UpdateMessage,
		LeftY:  gameState.PanYLeft,
		RightY: gameState.PanYRight,
		BallX:  gameState.Ball.X,
		BallY:  gameState.Ball.Y,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Println("Error marshaling game state:", err)
		return
	}

	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	for client := range clients {
		err := client.WriteMessage(websocket.TextMessage, msgBytes)
		if err != nil {
			log.Println("Error broadcasting to client:", err)
			client.Close()
			delete(clients, client)
		}
	}
}

// Broadcast game over message
func broadcastGameOver(winner string) {
	gameState.Lock()
	defer gameState.Unlock()

	msg := Message{
		Type:   GameOverMsg,
		Winner: winner,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Println("Error marshaling game over message:", err)
		return
	}

	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	for client := range clients {
		err := client.WriteMessage(websocket.TextMessage, msgBytes)
		if err != nil {
			log.Println("Error broadcasting game over to client:", err)
			client.Close()
			delete(clients, client)
		}
	}
}

// Assign players to paddles
var assignedPlayers = make(map[*websocket.Conn]string)
var assignMutex = sync.Mutex{}

// Assign a player to a paddle
func assignPlayer(conn *websocket.Conn) (string, error) {
	assignMutex.Lock()
	defer assignMutex.Unlock()

	// Check current assignments
	roles := map[string]bool{"left": false, "right": false}
	for _, role := range assignedPlayers {
		if role == "left" {
			roles["left"] = true
		}
		if role == "right" {
			roles["right"] = true
		}
	}

	var assigned string
	if !roles["left"] {
		assigned = "left"
	} else if !roles["right"] {
		assigned = "right"
	} else {
		assigned = "none" // Max two players
	}

	if assigned != "none" {
		assignedPlayers[conn] = assigned
		log.Printf("Assigned player %s to %s paddle", conn.RemoteAddr(), assigned)
	} else {
		log.Printf("No available paddle for player %s", conn.RemoteAddr())
	}

	return assigned, nil
}

// Clamp Y position within bounds
func clampYPosition(y int) int {
	if y < 0 {
		return 0
	}
	if y > MaxPaddleY {
		return MaxPaddleY
	}
	return y
}

// Handle incoming WebSocket connections
func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer ws.Close()

	// Assign player
	player, err := assignPlayer(ws)
	if err != nil {
		log.Println("Player assignment error:", err)
		return
	}

	if player == "none" {
		// Inform client no slot available
		msg := Message{
			Type:   ErrorMessage,
			Player: "none",
		}
		ws.WriteJSON(msg)
		return
	}

	// Add to clients
	clientsMutex.Lock()
	clients[ws] = player
	clientsMutex.Unlock()

	// Send assign message
	assignMsg := Message{
		Type:   AssignMessage,
		Player: player,
	}
	if err := ws.WriteJSON(assignMsg); err != nil {
		log.Println("Error sending assign message:", err)
	}

	// Send initial game state
	gameState.Lock()
	initialMsg := Message{
		Type:   UpdateMessage,
		LeftY:  gameState.PanYLeft,
		RightY: gameState.PanYRight,
		BallX:  gameState.Ball.X,
		BallY:  gameState.Ball.Y,
	}
	gameState.Unlock()
	if err := ws.WriteJSON(initialMsg); err != nil {
		log.Println("Error sending initial game state:", err)
	}

	log.Printf("Player %s connected. Assigned to %s paddle.", ws.RemoteAddr(), player)

	// Listen for messages
	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Read error from %s: %v", ws.RemoteAddr(), err)
			break
		}

		log.Printf("Received message from %s: %+v", ws.RemoteAddr(), msg)

		if msg.Type == MoveMessage && msg.Player != "" && msg.Y != nil {
			gameState.Lock()
			if msg.Player == "left" {
				// Clamp Y position
				clampedY := clampYPosition(*msg.Y)
				if clampedY != gameState.PanYLeft {
					gameState.PanYLeft = clampedY
					log.Printf("Updated left paddle Y to %d", gameState.PanYLeft)
				}
			} else if msg.Player == "right" {
				// Clamp Y position
				clampedY := clampYPosition(*msg.Y)
				if clampedY != gameState.PanYRight {
					gameState.PanYRight = clampedY
					log.Printf("Updated right paddle Y to %d", gameState.PanYRight)
				}
			}
			gameState.Unlock()

			// No immediate broadcast; game loop handles broadcasting
		} else {
			log.Printf("Invalid message from %s: %+v", ws.RemoteAddr(), msg)
		}
	}

	// Remove client on disconnect
	clientsMutex.Lock()
	delete(clients, ws)
	clientsMutex.Unlock()

	assignMutex.Lock()
	delete(assignedPlayers, ws)
	assignMutex.Unlock()

	log.Printf("Player %s disconnected.", ws.RemoteAddr())
}

// gameLoop updates the ball's position and broadcasts the game state
func gameLoop() {
	for {
		<-ticker.C
		updateBallPosition()
		broadcastGameState()
	}
}

// updateBallPosition updates the ball's position and handles collisions
func updateBallPosition() {
	gameState.Lock()
	defer gameState.Unlock()

	// Update ball position
	gameState.Ball.X += gameState.Ball.Vx
	gameState.Ball.Y += gameState.Ball.Vy

	// Collision with top wall
	if gameState.Ball.Y <= 0 {
		gameState.Ball.Y = 0
		gameState.Ball.Vy = -gameState.Ball.Vy
	}

	// Collision with bottom wall
	if gameState.Ball.Y >= float64(CanvasHeight) {
		gameState.Ball.Y = float64(CanvasHeight)
		gameState.Ball.Vy = -gameState.Ball.Vy
	}

	// Collision with left paddle
	if gameState.Ball.X <= float64(PaddleWidth) {
		if int(gameState.Ball.Y) >= gameState.PanYLeft && int(gameState.Ball.Y) <= (gameState.PanYLeft+PaddleHeight) {
			gameState.Ball.X = float64(PaddleWidth)
			gameState.Ball.Vx = -gameState.Ball.Vx
		}
	}

	// Collision with right paddle
	if gameState.Ball.X >= float64(CanvasWidth-PaddleWidth) {
		if int(gameState.Ball.Y) >= gameState.PanYRight && int(gameState.Ball.Y) <= (gameState.PanYRight+PaddleHeight) {
			gameState.Ball.X = float64(CanvasWidth - PaddleWidth)
			gameState.Ball.Vx = -gameState.Ball.Vx
		}
	}

	// Check for game over
	if gameState.Ball.X < 0 {
		// Ball touched the left wall, right player wins
		broadcastGameOver("right")
		resetGame()
	}
	if gameState.Ball.X > float64(CanvasWidth) {
		// Ball touched the right wall, left player wins
		broadcastGameOver("left")
		resetGame()
	}
}

// resetGame resets the ball to the center after a game over
func resetGame() {
	gameState.Ball.X = float64(CanvasWidth / 2)
	gameState.Ball.Y = float64(CanvasHeight / 2)
	// Reset velocity; you can randomize direction if desired
	gameState.Ball.Vx = 4.0
	gameState.Ball.Vy = 4.0
}

func main() {
	// Set up the WebSocket route
	http.HandleFunc("/ws", handleConnections)

	// Serve static files from the "public" directory
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	// Start the game loop
	go gameLoop()

	// Start the server
	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
