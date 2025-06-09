package chat

import (
	. "clack/common"
	"clack/common/snowflake"
	"clack/storage"
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"zombiezen.com/go/sqlite"
)

func (c *GatewayConnection) HandleError(err error) {
	code := ErrorCodeInternalError
	msg := err.Error()

	if cerr, ok := err.(*CodedError); ok {
		code = cerr.Code
		msg = cerr.Message
	}

	if code == ErrorCodeInternalError {
		gwLog.Println("Internal error:", msg, string(debug.Stack()))
	}

	c.Write(Event{
		Type: EventTypeErrorResponse,
		Data: ErrorResponse{
			Code:    code,
			Request: c.request,
		},
	})
}

func (c *GatewayConnection) HandleSettingsRequest(db *sqlite.Conn) {
	settings, err := storage.GetSettings(db)
	if err != nil {
		c.HandleError(err)
		return
	}

	preliminary := Event{
		Type: EventTypeSettingsResponse,
		Data: SettingsResponse{
			SiteName:           settings.SiteName,
			LoginMessage:       settings.LoginMessage,
			DefaultPermissions: settings.DefaultPermissions,
			Authenticated:      c.Authenticated(),
			UsesEmail:          settings.UsesEmail,
			UsesInviteCodes:    settings.UsesInviteCodes,
			UsesCaptcha:        settings.UsesCaptcha,
			UsesLoginCaptcha:   settings.UsesLoginCaptcha,
			CaptchSiteKey:      settings.CaptchaSiteKey,
		},
	}

	c.Write(preliminary)
}

func (c *GatewayConnection) HandleLoginRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req LoginRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	settings, err := storage.GetSettings(db)
	if err != nil {
		c.HandleError(err)
		return
	}

	if settings.UsesLoginCaptcha {
		verified, err := hCaptchaVerify(req.CaptchaResponse, c.ClientIP(), settings.CaptchaSiteKey, settings.CaptchaSecretKey)
		if !verified {
			c.HandleError(err)
			return
		}
	}

	userID, token, err := storage.Login(db, req.Username, req.Password)
	if err != nil {
		c.HandleError(err)
		return
	}

	c.OnAuthentication(userID, token)

	c.Write(Event{
		Type: EventTypeTokenResponse,
		Data: TokenResponse{
			Token: token,
		},
	})
}

func (c *GatewayConnection) HandleRegisterRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req RegisterRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	settings, err := storage.GetSettings(db)
	if err != nil {
		c.HandleError(err)
		return
	}

	if settings.UsesCaptcha {
		verified, err := hCaptchaVerify(req.CaptchaResponse, c.ClientIP(), settings.CaptchaSiteKey, settings.CaptchaSecretKey)
		if !verified {
			c.HandleError(err)
			return
		}
	}

	fmt.Println("Registering user", req.Username, req.Password, req.Email, req.InviteCode)

	userID, token, err := storage.Register(db, req.Username, req.Password, req.Email, req.InviteCode)
	if err != nil {
		c.HandleError(err)
		return
	}

	fmt.Println("Registered user", userID, token)

	c.OnAuthentication(userID, token)

	c.Write(Event{
		Type: EventTypeTokenResponse,
		Data: TokenResponse{
			Token: token,
		},
	})
}

func (c *GatewayConnection) HandleOverviewRequest(db *sqlite.Conn) {
	index := GetUserIndex(db)

	userList := index.GetUserListSlice(UserListRequest{
		StartGroup: -1,
		StartIndex: 0,
		EndGroup:   -1,
		EndIndex:   0,
	}, 20)

	users := []User{}
	for _, group := range userList.Groups {
		for _, id := range group.Users {
			user, ok := index.GetUser(id)
			if !ok {
				continue
			}
			users = append(users, user)
		}
	}

	roles := index.GetAllRoles()

	channels := storage.GetAllChannels(db)

	you, _ := storage.GetUser(db, c.userID)

	overview := Event{
		Type: EventTypeOverviewResponse,
		Data: OverviewResponse{
			You:      you,
			Channels: channels,
			Roles:    roles,
			UserList: userList,
			Users:    users,
		},
	}

	c.Write(overview)
}

func (c *GatewayConnection) HandleMessagesRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req MessagesRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	anchor := req.Before
	before := true
	if req.After != 0 {
		anchor = req.After
		before = false
	}

	msgs, err := storage.GetMessages(db, req.ChannelID, anchor, req.Limit, before)
	if err != nil {
		c.HandleError(err)
		return
	}

	c.Write(Event{
		Type: EventTypeMessagesResponse,
		Data: MessagesResponse{
			ChannelID: req.ChannelID,
			Before:    req.Before,
			After:     req.After,
			Limit:     req.Limit,
			Messages:  msgs,
		},
	})
}

func (c *GatewayConnection) HandleUsersRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req UsersRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	index := GetUserIndex(db)

	resp := UsersResponse{
		Users: index.GetUsers(req.Users),
	}

	c.Write(Event{
		Type: EventTypeUsersResponse,
		Data: resp,
	})
}

func (c *GatewayConnection) HandleUserListRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req UserListRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	index := GetUserIndex(db)
	resp := index.GetUserListSlice(req, 128)

	c.Write(Event{
		Type: EventTypeUserListResponse,
		Data: resp,
	})
}

func (c *GatewayConnection) HandleMessageSendRequest(msg *UnknownEvent, db *sqlite.Conn) {

	fmt.Println("HandleMessageSendRequest")

	var req MessageSendRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	permissions := storage.GetPermissions(db, c.userID, req.ChannelID)
	canSendMessages := permissions&PermissionSendMessages != 0
	canEmbedLinks := permissions&PermissionEmbedLinks != 0
	canUploadFiles := permissions&PermissionUploadFiles != 0

	if !canSendMessages {
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	if !canUploadFiles && req.AttachmentCount > 0 {
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	var full Message
	full.ID = snowflake.New()
	full.AuthorID = c.userID
	full.ChannelID = req.ChannelID
	full.Content = req.Content
	full.Type = MessageTypeDefault
	full.Timestamp = int(time.Now().UnixMilli())

	mentionedUsers, mentionedRoles, mentionedChannels, embeddableURLs := ParseMessageContent(req.Content)

	if !canEmbedLinks {
		embeddableURLs = nil
	}

	full.MentionedUsers = mentionedUsers
	full.MentionedRoles = mentionedRoles
	full.MentionedChannels = mentionedChannels
	full.EmbeddableURLs = embeddableURLs

	if req.AttachmentCount > 0 {
		slotID := snowflake.New()
		pending := PendingRequest{
			slotID:  slotID,
			message: full,
			seq:     msg.Seq,
			session: c.session,
		}

		gw.PushPendingRequest(&pending, slotID)

		c.Write(Event{
			Type: EventTypeMessageSendResponse,
			Seq:  pending.seq,
			Data: MessageUploadSlotResponse{
				SlotID: slotID,
			},
		})
	} else {
		c.HandleMessageSendRequestComplete(&full, msg.Seq, db)
	}
}
func (c *GatewayConnection) HandleMessageSendRequestComplete(rawMessage *Message, seq string, db *sqlite.Conn) {
	storage.AddMessage(db, rawMessage)

	message, err := storage.GetMessage(db, rawMessage.ID)

	message.EmbeddableURLs = rawMessage.EmbeddableURLs

	user, err := storage.GetUser(db, message.AuthorID)
	if err != nil {
		c.HandleError(err)
		return
	}

	c.Write(Event{
		Type: EventTypeMessageSendResponse,
		Seq:  seq,
		Data: MessageSendResponse{
			MessageID: message.ID,
		},
	})

	gw.OnMessageAdd(&MessageAddEvent{
		Message: message,
		Author:  user,
	})

	if len(message.EmbeddableURLs) > 0 {
		go c.TryEmbedURLs(message.ID, message.EmbeddableURLs, db)
	}
}

func (c *GatewayConnection) HandleMessageUpdateRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req MessageUpdateRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	full, err := storage.GetMessage(db, req.MessageID)
	if err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	if full.AuthorID != c.userID {
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	permissions := storage.GetPermissions(db, c.userID, full.ChannelID)
	canEmbedLinks := permissions&PermissionEmbedLinks != 0

	mentionedUsers, mentionedRoles, mentionedChannels, embeddableURLs := ParseMessageContent(req.Content)

	deletedEmbeds := make([]Snowflake, 0)
	addedURLs := make([]string, 0)

	for _, embed := range full.Embeds {
		found := false
		for _, url := range embeddableURLs {
			if embed.URL == url {
				found = true
				break
			}
		}
		if !found {
			deletedEmbeds = append(deletedEmbeds, embed.ID)
		}
	}

	for _, id := range embeddableURLs {
		found := false
		for _, embed := range full.Embeds {
			if embed.URL == id {
				found = true
				break
			}
		}
		if !found {
			addedURLs = append(addedURLs, id)
		}
	}

	if err := storage.UpdateMessage(db, req.MessageID, req.Content, mentionedUsers, mentionedRoles, mentionedChannels, deletedEmbeds); err != nil {
		c.HandleError(err)
		return
	}

	full, err = storage.GetMessage(db, req.MessageID)
	if err != nil {
		c.HandleError(err)
		return
	}

	full.EmbeddableURLs = addedURLs

	gw.OnMessageUpdate(&MessageUpdateEvent{
		Message: full,
	})

	if canEmbedLinks {
		go c.TryEmbedURLs(req.MessageID, addedURLs, db)
	}

}

func (c *GatewayConnection) HandleMessageDeleteRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req MessageDeleteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	if msg, err := storage.GetMessage(db, req.MessageID); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	} else {
		if msg.AuthorID != c.userID {
			var permissions = storage.GetPermissions(db, c.userID, msg.ChannelID)
			if permissions&PermissionManageMessages == 0 {
				c.HandleError(NewError(ErrorCodeNoPermission, nil))
				return
			}
		}
	}

	if err := storage.DeleteMessage(db, req.MessageID); err != nil {
		c.HandleError(err)
		return
	}

	gw.Relay(Event{
		Type: EventTypeMessageDelete,
		Data: MessageDeleteEvent{
			MessageID: req.MessageID,
		},
	})
}

func (c *GatewayConnection) TryEmbedURLs(id Snowflake, urls []string, db *sqlite.Conn) {
	for _, url := range urls {
		embed, err := GetEmbedFromURL(context.Background(), db, url)
		if err != nil || embed == nil {
			fmt.Println("Failed to get embed from URL:", err)
			continue
		}
		embed.ID = snowflake.New()

		storage.AddEmbed(db, id, embed)

	}
	message, err := storage.GetMessage(db, id)
	if err != nil {
		c.HandleError(err)
		return
	}
	gw.Relay(Event{
		Type: EventTypeMessageUpdate,
		Data: MessageUpdateEvent{
			Message: message,
		},
	})
}
