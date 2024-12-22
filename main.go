package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Constants for canvas and paddle dimensions
const (
	CanvasHeight = 600
	PaddleHeight = 100
	MaxPaddleY   = CanvasHeight - PaddleHeight
)

// Message types
const (
	AssignMessage = "assign"
	MoveMessage   = "move"
	UpdateMessage = "update"
	ErrorMessage  = "error"
)

// Message structure
type Message struct {
	Type   string `json:"type"`
	Player string `json:"player,omitempty"`
	Y      *int   `json:"y,omitempty"` // Made Y a pointer to detect null
	LeftY  int    `json:"leftY,omitempty"`
	RightY int    `json:"rightY,omitempty"`
}

// Define the upgrader
var upgrader = websocket.Upgrader{
	// Allow all origins for simplicity. In production, restrict this.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Game state structure
type GameState struct {
	sync.Mutex
	PanYLeft  int
	PanYRight int
}

// Initialize game state
var gameState = GameState{
	PanYLeft:  CanvasHeight/2 - PaddleHeight/2, // 250
	PanYRight: CanvasHeight/2 - PaddleHeight/2, // 250
}

// Client management
var clients = make(map[*websocket.Conn]string)
var clientsMutex = sync.Mutex{}

// Broadcast function to send updates to all clients
func broadcastUpdate() {
	gameState.Lock()
	defer gameState.Unlock()

	updateMsg := Message{
		Type:   UpdateMessage,
		LeftY:  gameState.PanYLeft,
		RightY: gameState.PanYRight,
	}

	msgBytes, err := json.Marshal(updateMsg)
	if err != nil {
		log.Println("Error marshaling update message:", err)
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

// Assign players to paddles
var assignedPlayers = make(map[*websocket.Conn]string)
var assignMutex = sync.Mutex{}

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

// clampYPosition ensures that the Y position is within valid bounds
func clampYPosition(y int) int {
	if y < 0 {
		return 0
	}
	if y > MaxPaddleY {
		return MaxPaddleY
	}
	return y
}

// handleConnections handles incoming WebSocket connections
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

	// Send initial paddle positions
	gameState.Lock()
	initialMsg := Message{
		Type:   UpdateMessage,
		LeftY:  gameState.PanYLeft,
		RightY: gameState.PanYRight,
	}
	gameState.Unlock()
	if err := ws.WriteJSON(initialMsg); err != nil {
		log.Println("Error sending initial positions:", err)
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
				clampedY := clampYPosition(*msg.Y)
				if clampedY != gameState.PanYLeft {
					gameState.PanYLeft = clampedY
					log.Printf("Updated left paddle Y to %d", gameState.PanYLeft)
				}
			} else if msg.Player == "right" {
				clampedY := clampYPosition(*msg.Y)
				if clampedY != gameState.PanYRight {
					gameState.PanYRight = clampedY
					log.Printf("Updated right paddle Y to %d", gameState.PanYRight)
				}
			}
			gameState.Unlock()

			// Broadcast update to all clients
			broadcastUpdate()
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

func main() {
	// Set up the HTTP server
	http.HandleFunc("/ws", handleConnections)

	// Serve static files (HTML, JS, CSS) from the "public" directory
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	// Start the server on port 8080
	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
