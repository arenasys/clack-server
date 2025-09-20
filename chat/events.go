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
	EventTypeMessageReactionDelete      = iota
	EventTypeMessageReactionDeleteAll   = iota
	EventTypeMessageReactionDeleteEmoji = iota

	EventTypeMessageReactionUsersRequest  = iota
	EventTypeMessageReactionUsersResponse = iota

	EventTypeChannelAdd        = iota
	EventTypeChannelUpdate     = iota
	EventTypeChannelDelete     = iota
	EventTypeChannelPinsUpdate = iota

	EventTypeRoleAdd    = iota
	EventTypeRoleUpdate = iota
	EventTypeRoleDelete = iota

	EventTypeUserAdd    = iota
	EventTypeUserDelete = iota
	EventTypeUserUpdate = iota

	EventTypeUserPresence = iota
	EventTypeUserTyping   = iota

	EventTypeLoginRequest    = iota
	EventTypeTokenResponse   = iota
	EventTypeLogoutRequest   = iota
	EventTypeRegisterRequest = iota

	EventTypeUploadSlot = iota
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
	ChannelID  Snowflake `json:"channel"`
	Before     Snowflake `json:"before,omitempty"`
	After      Snowflake `json:"after,omitempty"`
	Limit      int       `json:"limit"`
	Messages   []Message `json:"messages"`
	References []Message `json:"references,omitempty"`
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
	ReferenceID     Snowflake `json:"reference,omitempty"`
	AttachmentCount int       `json:"attachmentCount"`
}

type MessageSendResponse struct {
	MessageID Snowflake `json:"message"`
}

type MessageUploadSlot struct {
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
	Message   Message `json:"message"`
	Reference Message `json:"reference,omitempty"`
	Author    User    `json:"author"`
}

type MessageUpdateEvent struct {
	Message Message `json:"message"`
}

type MessageDeleteEvent struct {
	MessageID Snowflake `json:"message"`
}

type ReactionAddRequest struct {
	MessageID Snowflake `json:"message"`
	EmojiID   Snowflake `json:"emoji"`
}
type ReactionAddEvent struct {
	MessageID Snowflake `json:"message"`
	UserID    Snowflake `json:"user"`
	EmojiID   Snowflake `json:"emoji"`
}

type ReactionDeleteRequest struct {
	MessageID Snowflake `json:"message"`
	EmojiID   Snowflake `json:"emoji"`
}

type ReactionDeleteEvent struct {
	MessageID Snowflake `json:"message"`
	UserID    Snowflake `json:"user"`
	EmojiID   Snowflake `json:"emoji"`
}

type ReactionDeleteAllEvent struct {
	MessageID Snowflake `json:"message"`
}

type ReactionDeleteEmojiEvent struct {
	MessageID Snowflake `json:"message"`
	EmojiID   Snowflake `json:"emoji"`
}

type ReactionUsersRequest struct {
	MessageID Snowflake `json:"message"`
	EmojiID   Snowflake `json:"emoji"`
}

type ReactionUsersResponse struct {
	MessageID Snowflake   `json:"message"`
	EmojiID   Snowflake   `json:"emoji"`
	Users     []Snowflake `json:"users"`
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

type UserUpdateRequest struct {
	UserID Snowflake `json:"user" validate:"required"`

	DisplayName    string `json:"displayName,omitempty"`
	StatusMessage  string `json:"statusMessage,omitempty"`
	ProfileMessage string `json:"profileMessage,omitempty"`
	ProfileColor   int    `json:"profileColor,omitempty"`
	AvatarModified int    `json:"avatarModified,omitempty"`

	SetName    bool `json:"setName" validate:"required"`
	SetProfile bool `json:"setProfile" validate:"required"`
	SetAvatar  bool `json:"setAvatar" validate:"required"`
}

type UserAddEvent struct {
	User User `json:"user"`
}

type UserDeleteEvent struct {
	UserID Snowflake `json:"user"`
}

type UserUpdateEvent struct {
	User User `json:"user"`
}

type RoleAddRequest struct {
	Name        string `json:"name" validate:"required"`
	Color       int    `json:"color" validate:"required"`
	Position    int    `json:"position" validate:"required"`
	Permissions int    `json:"permissions" validate:"required"`
	Hoisted     bool   `json:"hoisted" validate:"required"`
	Mentionable bool   `json:"mentionable" validate:"required"`
}

type RoleUpdateRequest struct {
	Role Role `json:"role" validate:"required"`
}

type RoleDeleteRequest struct {
	RoleID Snowflake `json:"role" validate:"required"`
}

type RoleAddEvent struct {
	Role Role `json:"role"`
}

type RoleUpdateEvent struct {
	Role Role `json:"role"`
}

type RoleDeleteEvent struct {
	RoleID Snowflake `json:"role"`
}
