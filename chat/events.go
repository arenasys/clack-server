package chat

import (
	. "clack/common"
	"encoding/json"
)

const (
	EventTypeErrorResponse = iota

	EventTypeSettingsResponse = iota
	EventTypeOverviewResponse = iota

	EventTypeMessagesRequest  = iota
	EventTypeMessagesResponse = iota

	EventTypeUsersRequest  = iota
	EventTypeUsersResponse = iota

	EventTypeUserListRequest  = iota
	EventTypeUserListResponse = iota

	EventTypeMessageSendRequest  = iota
	EventTypeMessageSendResponse = iota

	EventTypeMessageAdd        = iota
	EventTypeMessageUpdate     = iota
	EventTypeMessageDelete     = iota
	EventTypeMessageDeleteBulk = iota

	EventTypeMessageReactionAdd         = iota
	EventTypeMessageReactionRemove      = iota
	EventTypeMessageReactionRemoveAll   = iota
	EventTypeMessageReactionRemoveEmoji = iota

	EventTypeChannelAdd        = iota
	EventTypeChannelUpdate     = iota
	EventTypeChannelDelete     = iota
	EventTypeChannelPinsUpdate = iota

	EventTypeRoleAdd    = iota
	EventTypeRoleUpdate = iota
	EventTypeRoleDelete = iota

	EventTypeUserAdd    = iota
	EventTypeUserRemove = iota
	EventTypeUserUpdate = iota

	EventTypeUserPresence = iota
	EventTypeUserTyping   = iota

	EventTypeLoginRequest    = iota
	EventTypeTokenResponse   = iota
	EventTypeLogoutRequest   = iota
	EventTypeRegisterRequest = iota

	EventTypeUploadSlotResponse = iota
)

type UnknownEvent struct {
	Type int             `json:"type"`
	Seq  string          `json:"seq,omitempty"`
	Data json.RawMessage `json:"data"`
}

type Event struct {
	Type int         `json:"type"`
	Seq  string      `json:"seq,omitempty"`
	Data interface{} `json:"data"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Request int    `json:"request"`
	Message string `json:"message,omitempty"`
}

type SettingsResponse struct {
	SiteName           string `json:"siteName"`
	LoginMessage       string `json:"loginMessage"`
	DefaultPermissions int    `json:"defaultPermissions"`
	Authenticated      bool   `json:"authenticated"`
	UsesEmail          bool   `json:"usesEmail"`
	UsesInviteCodes    bool   `json:"usesInviteCodes"`
	UsesCaptcha        bool   `json:"usesCaptcha"`
	UsesLoginCaptcha   bool   `json:"usesLoginCaptcha"`
	CaptchSiteKey      string `json:"captchaSiteKey,omitempty"`
}

type OverviewResponse struct {
	You      User             `json:"you"`
	Users    []User           `json:"users"`
	Channels []Channel        `json:"channels"`
	Roles    []Role           `json:"roles"`
	UserList UserListResponse `json:"userList"`
}

type MessagesRequest struct {
	ChannelID Snowflake `json:"channel"`
	Before    Snowflake `json:"before"`
	After     Snowflake `json:"after"`
	Limit     int       `json:"limit"`
}

type MessagesResponse struct {
	ChannelID Snowflake `json:"channel"`
	Before    Snowflake `json:"before,omitempty"`
	After     Snowflake `json:"after,omitempty"`
	Limit     int       `json:"limit"`
	Messages  []Message `json:"messages"`
}

type UsersRequest struct {
	Users []Snowflake `json:"users"`
}

type UsersResponse struct {
	Users []User `json:"users"`
}

type UserListRequest struct {
	StartGroup Snowflake `json:"startGroup"`
	StartIndex int       `json:"startIndex"`
	EndGroup   Snowflake `json:"endGroup"`
	EndIndex   int       `json:"endIndex"`
}

type UserListGroup struct {
	ID    Snowflake   `json:"id"`
	Count int         `json:"count"`
	Start int         `json:"start"`
	Users []Snowflake `json:"users"`
}

type UserListResponse struct {
	StartGroup Snowflake `json:"startGroup"`
	StartIndex int       `json:"startIndex"`
	EndGroup   Snowflake `json:"endGroup"`
	EndIndex   int       `json:"endIndex"`

	Groups []UserListGroup `json:"groups"`
}

type MessageSendRequest struct {
	ChannelID       Snowflake `json:"channel"`
	Content         string    `json:"content"`
	AttachmentCount int       `json:"attachmentCount"`
}

type MessageSendResponse struct {
	MessageID Snowflake `json:"message"`
}

type MessageUploadSlotResponse struct {
	SlotID Snowflake `json:"slot,omitempty"`
}

type MessageDeleteRequest struct {
	MessageID Snowflake `json:"message"`
}

type MessageUpdateRequest struct {
	MessageID Snowflake `json:"message"`
	Content   string    `json:"content"`
}
type MessageAddEvent struct {
	Message Message `json:"message"`
	Author  User    `json:"author"`
}

type MessageUpdateEvent struct {
	Message Message `json:"message"`
}

type MessageDeleteEvent struct {
	MessageID Snowflake `json:"message"`
}

type LoginRequest struct {
	Username        string `json:"username" validate:"required"`
	Password        string `json:"password" validate:"required"`
	CaptchaResponse string `json:"captchaResponse,omitempty"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type LogoutRequest struct{}

type RegisterRequest struct {
	Username        string `json:"username" validate:"required"`
	Password        string `json:"password" validate:"required"`
	Email           string `json:"email,omitempty"`
	InviteCode      string `json:"inviteCode,omitempty"`
	CaptchaResponse string `json:"captchaResponse,omitempty"`
}
