package common

import (
	"clack/common/snowflake"
)

type Snowflake = snowflake.Snowflake

const SnowflakeNone Snowflake = 0

const (
	UserStatusOffline = iota
	UserStatusOnline  = iota
	UserStatusAway    = iota
)

type User struct {
	ID       Snowflake   `json:"id" validate:"required"`
	Username string      `json:"username" validate:"required"`
	Nickname string      `json:"nickname,omitempty"`
	Status   int         `json:"status" validate:"required"`
	Roles    []Snowflake `json:"roles"`
}

func (u User) IsOnline() bool {
	return u.Status != UserStatusOffline
}

func (u User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Username
}

const (
	PermissionAdministrator  = 1 << iota
	PermissionInviteMembers  = 1 << iota
	PermissionSilenceMembers = 1 << iota
	PermissionKickMembers    = 1 << iota
	PermissionBanMembers     = 1 << iota

	PermissionSendMessages    = 1 << iota
	PermissionAddReactions    = 1 << iota
	PermissionEmbedLinks      = 1 << iota
	PermissionUploadFiles     = 1 << iota
	PermissionMentionEveryone = 1 << iota
	PermissionChangeNickname  = 1 << iota

	PermissionViewChannel        = 1 << iota
	PermissionReadMessageHistory = 1 << iota

	PermissionManageNicknames = 1 << iota
	PermissionManageMessages  = 1 << iota
	PermissionManageChannels  = 1 << iota
	PermissionManageRoles     = 1 << iota
	PermissionManageEmojis    = 1 << iota
)

const PermissionDefault = PermissionSendMessages |
	PermissionAddReactions |
	PermissionEmbedLinks |
	PermissionUploadFiles |
	PermissionChangeNickname |
	PermissionViewChannel |
	PermissionReadMessageHistory

const PermissionAll = 0x7FFFFFFF

const (
	OverwriteTypeRole = iota
	OverwriteTypeUser = iota
)

type Overwrite struct {
	ID    Snowflake `json:"id" validate:"required"`
	Type  int       `json:"type" validate:"required"`
	Allow int       `json:"allow" validate:"required"`
	Deny  int       `json:"deny" validate:"required"`
}

const (
	ChannelTypeText     = iota
	ChannelTypeVoice    = iota
	ChannelTypeCategory = iota
)

type Channel struct {
	ID          Snowflake   `json:"id" validate:"required"`
	Type        int         `json:"type" validate:"required"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	Position    int         `json:"position,omitempty"`
	ParentID    Snowflake   `json:"parent,omitempty"`
	Overwrites  []Overwrite `json:"overwrites,omitempty"`
}

type Role struct {
	ID          Snowflake `json:"id" validate:"required"`
	Name        string    `json:"name" validate:"required"`
	Color       int       `json:"color" validate:"required"`
	Position    int       `json:"position" validate:"required"`
	Permissions int       `json:"permissions" validate:"required"`
	Hoisted     bool      `json:"hoisted" validate:"required"`
	Mentionable bool      `json:"mentionable" validate:"required"`
}

type Emoji struct {
	Name string    `json:"name" validate:"required"`
	ID   Snowflake `json:"id,omitempty"`
}

const (
	MessageTypeDefault = iota
)

type Message struct {
	ID                Snowflake    `json:"id" validate:"required"`
	Type              int          `json:"type" validate:"required"`
	ChannelID         Snowflake    `json:"channel" validate:"required"`
	Timestamp         int          `json:"timestamp" validate:"required"`
	Pinned            bool         `json:"pinned,omitempty" validate:"required"`
	AuthorID          Snowflake    `json:"author" validate:"required"`
	ReferenceID       Snowflake    `json:"reference,omitempty"`
	Content           string       `json:"content" validate:"required"`
	EditedTimestamp   int          `json:"editedTimestamp,omitempty"`
	Attachments       []Attachment `json:"attachments,omitempty"`
	Embeds            []Embed      `json:"embeds,omitempty"`
	Reactions         []Reaction   `json:"reactions,omitempty"`
	MentionedUsers    []Snowflake  `json:"mentionedUsers,omitempty"`
	MentionedRoles    []Snowflake  `json:"mentionedRoles,omitempty"`
	MentionedChannels []Snowflake  `json:"mentionedChannels,omitempty"`
	EmbeddableURLs    []string     `json:"embeddableURLs,omitempty"`
}

const (
	EmbedTypeRich  = iota
	EmbedTypeImage = iota
	EmbedTypeVideo = iota
)

type Embed struct {
	ID          Snowflake      `json:"id" validate:"required"`
	Type        int            `json:"type" validate:"required"`
	URL         string         `json:"url" validate:"required"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Timestamp   int            `json:"timestamp,omitempty"`
	Image       *EmbedMedia    `json:"image,omitempty"`
	Thumbnail   *EmbedMedia    `json:"thumbnail,omitempty"`
	Video       *EmbedMedia    `json:"video,omitempty"`
	Author      *EmbedAuthor   `json:"author,omitempty"`
	Provider    *EmbedProvider `json:"provider,omitempty"`
	Footer      *EmbedFooter   `json:"footer,omitempty"`
	Fields      []EmbedField   `json:"fields,omitempty"`
}

type EmbedMedia struct {
	ID      Snowflake `json:"id,omitempty"`
	URL     string    `json:"url" validate:"required"`
	Width   int       `json:"width" validate:"required"`
	Height  int       `json:"height" validate:"required"`
	Preload string    `json:"preload,omitempty"`
}

type EmbedFooter struct {
	Text string      `json:"text" validate:"required"`
	Icon *EmbedMedia `json:"icon,omitempty"`
}

type EmbedAuthor struct {
	Name string      `json:"name" validate:"required"`
	URL  string      `json:"url,omitempty"`
	Icon *EmbedMedia `json:"icon,omitempty"`
}

type EmbedProvider struct {
	Name string `json:"name" validate:"required"`
	URL  string `json:"url,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name" validate:"required"`
	Value  string `json:"value" validate:"required"`
	Inline bool   `json:"inline" validate:"required"`
}

type Reaction struct {
	EmojiID Snowflake   `json:"emoji" validate:"required"`
	Count   int         `json:"count" validate:"required"`
	Users   []Snowflake `json:"users",omitempty`
}

const (
	AttachmentTypeFile  = iota
	AttachmentTypeImage = iota
	AttachmentTypeVideo = iota
)

type Attachment struct {
	ID       Snowflake `json:"id" validate:"required"`
	Filename string    `json:"filename" validate:"required"`
	Type     int       `json:"type" validate:"required"`
	MimeType string    `json:"mimetype" validate:"required"`
	Size     int       `json:"size" validate:"required"`
	Preload  string    `json:"preload,omitempty"`
	Width    int       `json:"width,omitempty"`
	Height   int       `json:"height,omitempty"`
}

type Settings struct {
	SiteName           string `json:"siteName"`
	LoginMessage       string `json:"loginMessage"`
	DefaultPermissions int    `json:"defaultPermissions"`
	UsesEmail          bool   `json:"usesEmail"`
	UsesInviteCodes    bool   `json:"usesInviteCodes"`
	UsesCaptcha        bool   `json:"usesCaptcha"`
	UsesLoginCaptcha   bool   `json:"usesLoginCaptcha"`
	CaptchaSiteKey     string `json:"captchaSiteKey"`
	CaptchaSecretKey   string `json:"-"`
}

const (
	ErrorCodeInternalError      = iota
	ErrorCodeInvalidRequest     = iota
	ErrorCodeInvalidToken       = iota
	ErrorCodeInvalidCredentials = iota
	ErrorCodeInvalidUsername    = iota
	ErrorCodeTakenUsername      = iota
	ErrorCodeInvalidInviteCode  = iota
	ErrorCodeInvalidCaptcha     = iota
	ErrorCodeNoPermission       = iota
	ErrorCodeConnectionClosing  = iota
)
