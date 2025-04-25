package network

import (
	"clack/chat"
	"clack/common/snowflake"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func gatewayHandler(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Sec-WebSocket-Protocol")
	upgrader.Subprotocols = append(upgrader.Subprotocols, token)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	chat.HandleGatewayConnection(srvCtx, conn, token)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	slotID, err := strconv.ParseInt(vars["slot_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid slot id", http.StatusBadRequest)
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "failed to create multipart reader", http.StatusBadRequest)
	}

	err = chat.HandleGatewayUpload(srvCtx, snowflake.Snowflake(slotID), reader)
	if err != nil {
		http.Error(w, "failed to handle upload", http.StatusInternalServerError)
		return
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
}

func buildAPIRouter(router *mux.Router) {
	router.HandleFunc("/gateway", gatewayHandler)

	router.HandleFunc("/upload/{slot_id}", uploadHandler)

	router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	router.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}
