package chat

import (
	. "clack/common"
	"clack/storage"
	"io"
	"strings"

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
	slotID      Snowflake
	requestType int
	requestData interface{}
	seq         string
	session     string
}

type Gateway struct {
	connections      map[string]*GatewayConnection
	connectionsMutex sync.RWMutex

	pending      map[Snowflake]*PendingRequest
	pendingMutex sync.RWMutex
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
	gw.connectionsMutex.RLock()
	defer gw.connectionsMutex.RUnlock()
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

	seq     string
	request int

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
	tx := storage.NewTransaction(db)
	tx.Start()
	userID, err := tx.Authenticate(token)
	tx.Commit(err)

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
	db, _ := storage.OpenConnection(c.ctx)
	defer storage.CloseConnection(db)

	c.TryAuthenticate(token, db)
	c.HandleSettingsRequest(db)

	if c.Authenticated() {
		c.HandleOverviewRequest(db)
	}
}

func (c *GatewayConnection) Process() {
	msg, err := c.Read()
	if err != nil {
		c.closing = true
		return
	}

	c.seq = msg.Seq
	c.request = msg.Type

	db, _ := storage.OpenConnection(c.ctx)

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
		case EventTypeMessageUpdate:
			c.HandleMessageUpdateRequest(msg, db)
			break
		case EventTypeMessageDelete:
			c.HandleMessageDeleteRequest(msg, db)
			break
		case EventTypeMessageReactionAdd:
			c.HandleMessageReactionAddRequest(msg, db)
			break
		case EventTypeMessageReactionDelete:
			c.HandleMessageReactionDeleteRequest(msg, db)
			break
		case EventTypeMessageReactionUsersRequest:
			c.HandleMessageReactionUsersRequest(msg, db)
			break
	case EventTypeUserUpdate:
		c.HandleUserUpdateRequest(msg, db)
		break
	case EventTypeRoleAdd:
		c.HandleRoleAddRequest(msg, db)
		break
	case EventTypeRoleUpdate:
		c.HandleRoleUpdateRequest(msg, db)
		break
	case EventTypeRoleDelete:
		c.HandleRoleDeleteRequest(msg, db)
		break
	default:
			c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		}
	}

	storage.CloseConnection(db)
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
		c.Process()
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

type UploadReader struct {
	mr *multipart.Reader
}

func (u *UploadReader) ReadFiles(callback func(metadata string, reader FileInputReader) error) error {
	var rawMetadata string

	var metadata struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}

	for {
		part, err := u.mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading multipart part: %v", err)
		}

		name := part.FormName()

		if strings.HasPrefix(name, "metadata_") {
			buf := new(strings.Builder)

			if _, err := io.Copy(buf, part); err != nil {
				part.Close()
				return fmt.Errorf("reading metadata part %q: %v", name, err)
			}
			rawMetadata = buf.String()

			if err := json.Unmarshal([]byte(rawMetadata), &metadata); err != nil {
				part.Close()
				return fmt.Errorf("parsing metadata part %q: %v", name, err)
			}

			part.Close()
			continue
		}

		if strings.HasPrefix(name, "file_") {

			reader := NewPartReader(part, metadata.Size)

			err = callback(rawMetadata, reader)

			if err != nil {
				part.Close()
				return fmt.Errorf("callback file %q: %v", metadata.Filename, err)
			}

			part.Close()
			continue
		}
	}

	return nil
}

func HandleGatewayUpload(ctx context.Context, slotID Snowflake, mr *multipart.Reader) error {
	pending := gw.PopPendingRequest(slotID)
	if pending == nil {
		return fmt.Errorf("invalid slot id")
	}

	conn := gw.GetConnection(pending.session)
	if conn == nil {
		return fmt.Errorf("invalid session")
	}

	reader := UploadReader{mr: mr}

	switch pending.requestType {
	case EventTypeMessageSendRequest:
		conn.HandleMessageSendUpload(pending.requestData.(*Message), pending, &reader)
		break
	case EventTypeUserUpdate:
		conn.HandleUserUpdateUpload(pending.requestData.(*UserUpdateRequest), pending, &reader)
		break
	default:
		break
	}
	return nil

}

func init() {
	gw = &Gateway{
		connectionsMutex: sync.RWMutex{},
		connections:      make(map[string]*GatewayConnection),
		pendingMutex:     sync.RWMutex{},
		pending:          make(map[Snowflake]*PendingRequest),
	}
}
