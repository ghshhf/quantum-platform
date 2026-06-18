package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ghshhf/quantum-platform/backend/pkg/quantum"
)

var bridge *quantum.Bridge
var session *quantum.Session

type chatRequest struct { Message string `json:"message"`; Model string `json:"model"` }
type chatResponse struct { Content string `json:"content"`; Sources []string `json:"sources"` }

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "POST only", 405); return }
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w, err.Error(), 400); return }
	answer, err := bridge.Ask(context.Background(), session, req.Message)
	if err != nil { http.Error(w, err.Error(), 500); return }
	json.NewEncoder(w).Encode(chatResponse{Content: answer.Content, Sources: answer.InvokedEntities})
}

func main() {
	terminal := quantum.NewTerminalEntity("desktop-agent", quantum.NewLocalConnector(), nil)
	bridge = quantum.NewBridge(nil, quantum.DefaultSessionConfig())
	session = bridge.NewSession([16]byte{}, []quantum.Entity{terminal})

	// Determine frontend dist directory relative to the binary
	execPath, _ := os.Executable()
	baseDir := filepath.Dir(execPath)
	frontendDist := filepath.Join(baseDir, "frontend", "dist")
	if _, err := os.Stat(frontendDist); os.IsNotExist(err) {
		// Fallback: relative to CWD (development mode)
		frontendDist = "./frontend/dist"
	}
	fmt.Printf("Serving frontend from: %s\n", frontendDist)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", chatHandler)
	mux.Handle("/", http.FileServer(http.Dir(frontendDist)))
	s := &http.Server{Addr: ":8889", Handler: mux, ReadTimeout: 30*time.Second, WriteTimeout: 30*time.Second}
	fmt.Println("Quantum Platform Standalone on :8889")
	log.Fatal(s.ListenAndServe())
}
