package chat

import (
	. "clack/common"
	"clack/storage"
	"io"
	"strings"

	"clack/common/snowflake"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"zombiezen.com/go/sqlite"
)

var gwLog = NewLogger("GATEWAY")

var gw *Gateway

type PendingRequest struct {
	slotID  Snowflake
	message Message
	seq     string
	session string
}

type Gateway struct {
	connections      map[string]*GatewayConnection
	connectionsMutex sync.Mutex

	pending      map[Snowflake]*PendingRequest
	pendingMutex sync.Mutex
}

func (gw *Gateway) AddConnection(conn *GatewayConnection) {
	gw.connectionsMutex.Lock()
	gw.connections[conn.session] = conn
	gw.connectionsMutex.Unlock()
}

func (gw *Gateway) RemoveConnection(conn *GatewayConnection) {
	gw.connectionsMutex.Lock()
	delete(gw.connections, conn.session)
	gw.connectionsMutex.Unlock()
}

func (gw *Gateway) GetConnection(session string) *GatewayConnection {
	gw.connectionsMutex.Lock()
	defer gw.connectionsMutex.Unlock()
	if conn, ok := gw.connections[session]; ok {
		return conn
	}
	return nil
}

func (gw *Gateway) PushPendingRequest(req *PendingRequest, id Snowflake) {
	gw.pendingMutex.Lock()
	gw.pending[id] = req
	gw.pendingMutex.Unlock()
}

func (gw *Gateway) PopPendingRequest(id Snowflake) *PendingRequest {
	gw.pendingMutex.Lock()
	defer gw.pendingMutex.Unlock()
	if req, ok := gw.pending[id]; ok {
		delete(gw.pending, id)
		return req
	}
	return nil
}

type GatewayConnection struct {
	ws  *websocket.Conn
	ctx context.Context

	queue chan *Event

	userID  Snowflake
	token   string
	session string

	seq string

	closing bool

	writeMutex sync.Mutex
}

func (c *GatewayConnection) writeEvent(msg Event) {
	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()

	writer, _ := c.ws.NextWriter(websocket.TextMessage)

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.Encode(msg)

	writer.Close()
}

func (c *GatewayConnection) Write(msg Event) {
	msg.Seq = c.seq
	c.writeEvent(msg)
}

func (c *GatewayConnection) WriteUnsolicited(msg Event) {
	c.writeEvent(msg)
}

func (c *GatewayConnection) Read() (*UnknownEvent, error) {
	if c.ws == nil {
		return nil, fmt.Errorf("connection is nil")
	}

	_, reader, err := c.ws.NextReader()
	if err != nil {
		return nil, fmt.Errorf("failed to get reader: %v", err)
	}

	decoder := json.NewDecoder(reader)
	var msg UnknownEvent
	err = decoder.Decode(&msg)
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %v", err)
	}

	return &msg, nil
}

func (c *GatewayConnection) Close() error {
	err := c.ws.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second*5),
	)

	if err != nil {
		c.ws.Close()
		return fmt.Errorf("failed to close connection: %v", err)
	}

	if err := c.ws.SetReadDeadline(time.Now().Add(time.Second * 5)); err != nil {
		c.ws.Close()
		return fmt.Errorf("failed to set read deadline: %v", err)
	}

	return nil
}

func (c *GatewayConnection) Relay(event *Event) {
	if len(c.queue) >= cap(c.queue) {
		gwLog.Printf("Queue is full, dropping event: %v", event)
		// Close the connection?
		return
	}

	if !c.Connected() || !c.Authenticated() {
		return
	}

	c.queue <- event
}

func (c *GatewayConnection) ClientIP() string {
	if c.ws == nil {
		return ""
	}

	addr := c.ws.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)

	if err != nil {
		return ""
	}

	return host
}

func (c *GatewayConnection) TryAuthenticate(token string, db *sqlite.Conn) bool {
	userID, err := storage.Authenticate(db, token)
	if err != nil {
		fmt.Println("Failed to authenticate", err)
		c.HandleError(err)
		return false
	}

	c.OnAuthentication(userID, token)

	return true
}

func (c *GatewayConnection) OnAuthentication(userID Snowflake, token string) {
	c.userID = userID
	c.token = token
	c.session = GetRandom256()

	gw.AddConnection(c)
}

func (c *GatewayConnection) Connected() bool {
	if c.ws == nil {
		return false
	}

	if c.closing {
		return false
	}

	return true
}

func (c *GatewayConnection) Authenticated() bool {
	if c.token == "" {
		return false
	}

	if c.userID == 0 {
		return false
	}

	return true
}

func (c *GatewayConnection) Introduction(token string) {
	db, _ := storage.OpenDatabase(c.ctx)
	defer storage.CloseDatabase(db)

	c.TryAuthenticate(token, db)
	c.HandleSiteRequest(db)

	if c.Authenticated() {
		c.HandleOverviewRequest(db)
	}
}

func (c *GatewayConnection) Run(token string) {
	defer c.ws.Close()

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				c.Close()
				return
			case msg := <-c.queue:
				if msg != nil {
					c.WriteUnsolicited(*msg)
				}
			}
		}
	}()

	c.Introduction(token)

	for c.ctx.Err() == nil && !c.closing {
		msg, err := c.Read()
		if err != nil {
			c.closing = true
			break
		}

		c.seq = msg.Seq

		db, _ := storage.OpenDatabase(c.ctx)

		if !c.Authenticated() {
			switch msg.Type {
			case EventTypeLoginRequest:
				c.HandleLoginRequest(msg, db)
				break
			case EventTypeRegisterRequest:
				c.HandleRegisterRequest(msg, db)
				break
			default:
				c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
			}
		} else {
			switch msg.Type {
			case EventTypeMessagesRequest:
				c.HandleMessagesRequest(msg, db)
				break
			case EventTypeUsersRequest:
				c.HandleUsersRequest(msg, db)
				break
			case EventTypeUserListRequest:
				c.HandleUserListRequest(msg, db)
				break
			case EventTypeMessageSendRequest:
				c.HandleMessageSendRequest(msg, db)
				break
			default:
				c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
			}
		}

		storage.CloseDatabase(db)
	}
}

func HandleGatewayConnection(ctx context.Context, conn *websocket.Conn, token string) {
	gwLog.Printf("Connection from %s", conn.RemoteAddr().String())

	c := &GatewayConnection{
		ws:      conn,
		ctx:     ctx,
		session: GetRandom256(),
		queue:   make(chan *Event, 16),
		closing: false,
	}

	c.Run(token)

	gwLog.Printf("Connection from %s closed", conn.RemoteAddr().String())
}
func HandleGatewayUpload(ctx context.Context, slotID Snowflake, mr *multipart.Reader) error {
	gwLog.Printf("Upload from %s", ctx.Value("remote_addr"))

	pending := gw.PopPendingRequest(slotID)
	if pending == nil {
		return fmt.Errorf("invalid slot id")
	}

	conn := gw.GetConnection(pending.session)
	if conn == nil {
		return fmt.Errorf("invalid session")
	}

	db, _ := storage.OpenDatabase(ctx)
	defer storage.CloseDatabase(db)

	// Acknowledge the initial send response
	pending.message.ID = snowflake.New()
	conn.Write(Event{
		Type: EventTypeMessageSendResponse,
		Seq:  pending.seq,
		Data: MessageSendResponse{
			MessageID: pending.message.ID,
		},
	})

	var metadata struct {
		Filename  string `json:"filename"`
		Spoilered bool   `json:"spoilered"`
		Size      int64  `json:"size"`
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading multipart part: %v", err)
		}

		name := part.FormName()

		if strings.HasPrefix(name, "metadata_") {
			if err := json.NewDecoder(part).Decode(&metadata); err != nil {
				part.Close()
				return fmt.Errorf("invalid metadata %q: %v", name, err)
			}
			part.Close()
			continue
		}

		if strings.HasPrefix(name, "file_") {
			attachmentID := snowflake.New()
			storedName := storage.GetAttachmentFilename(attachmentID, metadata.Filename)

			reader := NewPartReader(part, metadata.Size)

			if err := storage.AddFile(db, storedName, reader); err != nil {
				part.Close()
				return fmt.Errorf("storing file %q: %v", metadata.Filename, err)
			}

			att, err := storage.BuildAttachmentFromFile(db, attachmentID, storedName)
			if err != nil {
				part.Close()
				return fmt.Errorf("building attachment %q: %v", metadata.Filename, err)
			}

			part.Close()

			pending.message.Attachments = append(pending.message.Attachments, *att)
			continue
		}

		part.Close()
	}

	// Finalize the send request with all collected attachments
	conn.HandleMessageSendRequestComplete(&pending.message, pending.seq, db)
	return nil
}

func init() {
	gw = &Gateway{
		connectionsMutex: sync.Mutex{},
		connections:      make(map[string]*GatewayConnection),
		pendingMutex:     sync.Mutex{},
		pending:          make(map[Snowflake]*PendingRequest),
	}
}
