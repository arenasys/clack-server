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
	tx := storage.NewTransaction(db)
	tx.Start()
	settings, err := tx.GetSettings()
	tx.Commit(err)

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

	tx := storage.NewTransaction(db)
	tx.Start()
	settings, err := tx.GetSettings()

	if err != nil {
		tx.Commit(err)
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

	userID, token, err := tx.Login(req.Username, req.Password)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

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

	tx := storage.NewTransaction(db)
	tx.Start()

	settings, err := tx.GetSettings()
	if err != nil {
		tx.Commit(err)
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

	user, token, err := tx.Register(req.Username, req.Password, req.Email, req.InviteCode)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	fmt.Println("Registered user", user.ID, token)

	index := GetUserIndex(db)
	index.AddUser(user)

	c.OnAuthentication(user.ID, token)

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

	tx := storage.NewTransaction(db)
	tx.Start()

	channels := tx.GetAllChannels()
	you, _ := tx.GetUser(c.userID)

	tx.Commit(nil)

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
	var err error = nil
	var msgs []Message = nil

	haveBefore := req.Before != 0
	haveAfter := req.After != 0

	tx := storage.NewTransaction(db)
	tx.Start()

	if haveBefore && haveAfter {
		if req.Before != req.After {
			c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
			return
		}

		var beforeMsgs []Message
		var afterMsgs []Message
		var anchorMsg Message

		beforeMsgs, err = tx.GetMessagesByAnchor(req.ChannelID, req.Before, req.Limit, true)
		anchorMsg, err = tx.GetMessage(req.Before)
		afterMsgs, err = tx.GetMessagesByAnchor(req.ChannelID, req.After, req.Limit, false)

		msgs = make([]Message, 0, len(beforeMsgs)+len(afterMsgs)+1)
		msgs = append(msgs, beforeMsgs...)
		msgs = append(msgs, anchorMsg)
		msgs = append(msgs, afterMsgs...)
	} else if haveBefore {
		msgs, err = tx.GetMessagesByAnchor(req.ChannelID, req.Before, req.Limit, true)
	} else if haveAfter {
		msgs, err = tx.GetMessagesByAnchor(req.ChannelID, req.After, req.Limit, false)
	} else {
		msgs, err = tx.GetMessagesByAnchor(req.ChannelID, 0, req.Limit, true)
	}

	if err != nil {
		tx.Commit(nil)
		c.HandleError(err)
		return
	}

	referenceIDs := make([]Snowflake, 0, len(msgs))
	for _, msg := range msgs {
		if msg.ReferenceID != 0 {
			referenceIDs = append(referenceIDs, msg.ReferenceID)
		}
	}

	references := []Message{}
	if len(referenceIDs) > 0 {
		references, err = tx.GetMessages(referenceIDs, false)
		if err != nil {
			tx.Commit(nil)
			c.HandleError(err)
			return
		}
	}

	tx.Commit(nil)

	c.Write(Event{
		Type: EventTypeMessagesResponse,
		Data: MessagesResponse{
			ChannelID:  req.ChannelID,
			Before:     req.Before,
			After:      req.After,
			Limit:      req.Limit,
			Messages:   msgs,
			References: references,
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
	var req MessageSendRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	permissions := storage.NewTransaction(db).GetPermissionsByChannel(c.userID, req.ChannelID)
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
	if req.ReferenceID != 0 {
		full.ReferenceID = req.ReferenceID
	}

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
			slotID:      slotID,
			requestData: &full,
			requestType: EventTypeMessageSendRequest,
			seq:         msg.Seq,
			session:     c.session,
		}

		gw.PushPendingRequest(&pending, slotID)

		c.Write(Event{
			Type: EventTypeUploadSlot,
			Seq:  pending.seq,
			Data: MessageUploadSlot{
				SlotID: slotID,
			},
		})
	} else {
		err := c.FinalizeMessageSendRequest(&full, msg.Seq, db)
		if err != nil {
			c.HandleError(err)
			return
		}
	}
}

func (c *GatewayConnection) HandleMessageSendUpload(message *Message, pending *PendingRequest, reader *UploadReader) {
	db, _ := storage.OpenConnection(c.ctx)
	defer storage.CloseConnection(db)

	message.ID = snowflake.New()

	err := reader.ReadFiles(func(metadata string, reader FileInputReader) error {
		var parsed struct {
			Filename  string `json:"filename"`
			Size      int64  `json:"size"`
			Spoilered bool   `json:"spoilered"`
		}

		if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
			return err
		}

		attachmentID := snowflake.New()

		attachment, err := storage.UploadAttachment(message.ID, attachmentID, parsed.Filename, reader)
		if err != nil {
			return err
		}
		message.Attachments = append(message.Attachments, *attachment)

		return nil
	})
	if err != nil {
		c.HandleError(err)
		return
	}

	err = c.FinalizeMessageSendRequest(message, pending.seq, db)
	if err != nil {
		c.HandleError(err)
		return
	}
}

func (c *GatewayConnection) FinalizeMessageSendRequest(rawMessage *Message, seq string, db *sqlite.Conn) error {
	tx := storage.NewTransaction(db)

	tx.Start()
	err := tx.AddMessage(rawMessage)
	if err != nil {
		tx.Commit(err)
		return err
	}

	message, err := tx.GetMessage(rawMessage.ID)
	if err != nil {
		tx.Commit(err)
		return err
	}

	message.EmbeddableURLs = rawMessage.EmbeddableURLs

	user, err := tx.GetUser(message.AuthorID)
	if err != nil {
		tx.Commit(err)
		return err
	}

	reference := Message{}
	if message.ReferenceID != 0 {
		reference, _ = tx.GetMessage(message.ReferenceID)
	}

	tx.Commit(nil)

	c.Write(Event{
		Type: EventTypeMessageSendResponse,
		Seq:  seq,
		Data: MessageSendResponse{
			MessageID: message.ID,
		},
	})

	gw.OnMessageAdd(&MessageAddEvent{
		Message:   message,
		Reference: reference,
		Author:    user,
	})

	if len(message.EmbeddableURLs) > 0 {
		go c.TryEmbedURLs(message.ID, message.EmbeddableURLs, db)
	}

	return nil
}

func (c *GatewayConnection) HandleMessageUpdateRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req MessageUpdateRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()

	full, err := tx.GetMessage(req.MessageID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	if full.AuthorID != c.userID {
		tx.Commit(nil)
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	permissions := tx.GetPermissionsByChannel(c.userID, full.ChannelID)
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

	if err := tx.SetMessage(req.MessageID, req.Content, mentionedUsers, mentionedRoles, mentionedChannels, deletedEmbeds); err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	full, err = tx.GetMessage(req.MessageID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

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

	tx := storage.NewTransaction(db)
	tx.Start()

	if msg, err := tx.GetMessage(req.MessageID); err != nil {
		tx.Commit(err)
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	} else {
		if msg.AuthorID != c.userID {
			var permissions = tx.GetPermissionsByChannel(c.userID, msg.ChannelID)
			if permissions&PermissionManageMessages == 0 {
				err := NewError(ErrorCodeNoPermission, nil)
				tx.Commit(err)
				c.HandleError(err)
				return
			}
		}
	}

	if err := tx.DeleteMessage(req.MessageID); err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	gw.Relay(Event{
		Type: EventTypeMessageDelete,
		Data: MessageDeleteEvent{
			MessageID: req.MessageID,
		},
	})
}

func (c *GatewayConnection) HandleMessageReactionAddRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req ReactionAddRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()

	permissions := tx.GetPermissionsByMessage(c.userID, req.MessageID)

	count, err := tx.GetReactionCount(req.MessageID, req.EmojiID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	if count == 0 && permissions&PermissionAddReactions == 0 {
		err := NewError(ErrorCodeNoPermission, nil)
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	if err := tx.AddReaction(req.MessageID, c.userID, req.EmojiID); err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	gw.Relay(Event{
		Type: EventTypeMessageReactionAdd,
		Data: ReactionAddEvent{
			MessageID: req.MessageID,
			UserID:    c.userID,
			EmojiID:   req.EmojiID,
		},
	})
}

func (c *GatewayConnection) HandleMessageReactionDeleteRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req ReactionDeleteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()

	if err := tx.DeleteReaction(req.MessageID, c.userID, req.EmojiID); err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	gw.Relay(Event{
		Type: EventTypeMessageReactionDelete,
		Data: ReactionDeleteEvent{
			MessageID: req.MessageID,
			UserID:    c.userID,
			EmojiID:   req.EmojiID,
		},
	})
}

func (c *GatewayConnection) HandleMessageReactionUsersRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req ReactionUsersRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()

	users, err := tx.GetReactionUsers(req.MessageID, req.EmojiID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	c.Write(Event{
		Type: EventTypeMessageReactionUsersResponse,
		Data: ReactionUsersResponse{
			MessageID: req.MessageID,
			EmojiID:   req.EmojiID,
			Users:     users,
		},
	})
}

func (c *GatewayConnection) HandleUserUpdateRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req UserUpdateRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()
	user, err := tx.GetUser(req.UserID)
	permissions, _ := tx.GetPermissionsByUser(c.userID)
	tx.Commit(nil)

	if err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	if user.ID == c.userID && permissions&PermissionChangeProfile == 0 {
		err := NewError(ErrorCodeNoPermission, nil)
		c.HandleError(err)
		return
	}

	// Setting other peoples profile, narrow what is permitted (only reseting)
	if user.ID != c.userID {
		if permissions&PermissionManageProfiles == 0 {
			err := NewError(ErrorCodeNoPermission, nil)
			c.HandleError(err)
			return
		}

		if permissions&PermissionAdministrator == 0 {
			if req.SetAvatar && req.AvatarModified != AvatarModifiedDefault {
				err := NewError(ErrorCodeNoPermission, nil)
				c.HandleError(err)
				return
			}

			if req.SetProfile {
				statusAllowed := req.StatusMessage == "" || req.StatusMessage == user.StatusMessage
				profileAllowed := req.ProfileMessage == "" || req.ProfileMessage == user.ProfileMessage
				colorAllowed := req.ProfileColor == ProfileColorDefault || req.ProfileColor == user.ProfileColor
				if !statusAllowed || !profileAllowed || !colorAllowed {
					err := NewError(ErrorCodeNoPermission, nil)
					tx.Commit(err)
					c.HandleError(err)
					return
				}
			}
		}
	}

	if !req.SetName {
		req.DisplayName = user.DisplayName
	}

	if !req.SetProfile {
		req.StatusMessage = user.StatusMessage
		req.ProfileMessage = user.ProfileMessage
		req.ProfileColor = user.ProfileColor
	}

	if !req.SetAvatar {
		req.AvatarModified = user.AvatarModified
	}

	if req.SetAvatar && req.AvatarModified != 0 {
		// They want to upload an avatar, send an upload slot and pend for it
		slotID := snowflake.New()
		pending := PendingRequest{
			slotID:      slotID,
			requestData: &req,
			requestType: EventTypeUserUpdate,
			seq:         msg.Seq,
			session:     c.session,
		}

		gw.PushPendingRequest(&pending, slotID)

		c.Write(Event{
			Type: EventTypeUploadSlot,
			Seq:  pending.seq,
			Data: MessageUploadSlot{
				SlotID: slotID,
			},
		})
	} else {
		c.FinalizeUserUpdateRequest(&req, db)
	}
}

func (c *GatewayConnection) HandleUserUpdateUpload(req *UserUpdateRequest, pending *PendingRequest, reader *UploadReader) {
	db, _ := storage.OpenConnection(c.ctx)
	defer storage.CloseConnection(db)

	modified := time.Now().UnixMilli()
	req.AvatarModified = int(modified)

	err := reader.ReadFiles(func(_ string, reader FileInputReader) error {
		storage.UploadAvatar(req.UserID, modified, reader)
		return nil
	})
	if err != nil {
		c.HandleError(err)
	}

	c.FinalizeUserUpdateRequest(req, db)
}

func (c *GatewayConnection) FinalizeUserUpdateRequest(req *UserUpdateRequest, db *sqlite.Conn) {
	tx := storage.NewTransaction(db)
	tx.Start()

	err := tx.SetUserProfile(c.userID, req.DisplayName, req.StatusMessage, req.ProfileMessage, req.ProfileColor, req.AvatarModified)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	user, err := tx.GetUser(c.userID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	// Update in-memory index and invalidate groups if needed
	index := GetUserIndex(db)
	index.UpdateUser(user)

	gw.Relay(Event{
		Type: EventTypeUserUpdate,
		Data: UserUpdateEvent{
			User: user,
		},
	})
}

func (c *GatewayConnection) TryEmbedURLs(id Snowflake, urls []string, db *sqlite.Conn) {
	tx := storage.NewTransaction(db)
	tx.Start()

	for _, url := range urls {
		embed, err := GetEmbedFromURL(context.Background(), id, url)
		if err != nil || embed == nil {
			fmt.Println("Failed to get embed from URL:", err)
			continue
		}
		embed.ID = snowflake.New()

		err = tx.AddEmbed(id, embed)
		if err != nil {
			tx.Commit(err)
			c.HandleError(err)
			return
		}
	}
	message, err := tx.GetMessage(id)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	gw.Relay(Event{
		Type: EventTypeMessageUpdate,
		Data: MessageUpdateEvent{
			Message: message,
		},
	})
}

// Role Management Handlers
func (c *GatewayConnection) HandleRoleAddRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req RoleAddRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()
	perms, _ := tx.GetPermissionsByUser(c.userID)
	if perms&PermissionManageRoles == 0 {
		tx.Commit(nil)
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	roleID, err := tx.AddRole(req.Name, req.Color, req.Position, req.Permissions, req.Hoisted, req.Mentionable)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	role, err := tx.GetRole(roleID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	// Update index cache
	index := GetUserIndex(db)
	index.Mutex.Lock()
	index.Roles[role.ID] = role
	index.Mutex.Unlock()
	index.InvalidateGroups()

	gw.Relay(Event{
		Type: EventTypeRoleAdd,
		Data: RoleAddEvent{Role: role},
	})
}

func (c *GatewayConnection) HandleRoleUpdateRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req RoleUpdateRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()
	perms, _ := tx.GetPermissionsByUser(c.userID)
	if perms&PermissionManageRoles == 0 {
		tx.Commit(nil)
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	role := req.Role
	if err := tx.UpdateRole(role.ID, role.Name, role.Color, role.Position, role.Permissions, role.Hoisted, role.Mentionable); err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	role, err := tx.GetRole(role.ID)
	if err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	index := GetUserIndex(db)
	index.Mutex.Lock()
	index.Roles[role.ID] = role
	index.Mutex.Unlock()
	index.InvalidateGroups()

	gw.Relay(Event{
		Type: EventTypeRoleUpdate,
		Data: RoleUpdateEvent{Role: role},
	})
}

func (c *GatewayConnection) HandleRoleDeleteRequest(msg *UnknownEvent, db *sqlite.Conn) {
	var req RoleDeleteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.HandleError(NewError(ErrorCodeInvalidRequest, nil))
		return
	}

	tx := storage.NewTransaction(db)
	tx.Start()
	perms, _ := tx.GetPermissionsByUser(c.userID)
	if perms&PermissionManageRoles == 0 {
		tx.Commit(nil)
		c.HandleError(NewError(ErrorCodeNoPermission, nil))
		return
	}

	if err := tx.DeleteRole(req.RoleID); err != nil {
		tx.Commit(err)
		c.HandleError(err)
		return
	}

	tx.Commit(nil)

	index := GetUserIndex(db)
	index.Mutex.Lock()
	delete(index.Roles, req.RoleID)
	index.Mutex.Unlock()
	index.InvalidateGroups()

	gw.Relay(Event{
		Type: EventTypeRoleDelete,
		Data: RoleDeleteEvent{RoleID: req.RoleID},
	})
}
