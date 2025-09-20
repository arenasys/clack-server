package storage

import (
	. "clack/common"
	"clack/common/emoji"
	"clack/common/snowflake"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

var txMutex sync.Mutex
var txLastWrite time.Time

type Transaction struct {
	conn    *sqlite.Conn
	commit  func(*error)
	isWrite bool
}

func NewTransaction(conn *sqlite.Conn) *Transaction {
	return &Transaction{
		conn:    conn,
		commit:  nil,
		isWrite: false,
	}
}

func (tx *Transaction) MarkAsWrite() *Transaction {
	tx.isWrite = true
	return tx
}

func (tx *Transaction) UpdateLastWrite() {
	txMutex.Lock()
	defer txMutex.Unlock()
	now := time.Now()
	if now.After(txLastWrite) {
		txLastWrite = now
	}
}

func (tx *Transaction) Start() {
	if tx.commit != nil {
		return
	}
	tx.commit = sqlitex.Transaction(tx.conn)
	if tx.commit == nil {
		panic("failed to begin transaction")
	}
}

func (tx *Transaction) Commit(err error) {
	if tx.commit == nil {
		return
	}

	var noError error = nil
	if err != nil {
		tx.commit(&err)
	} else {
		tx.commit(&noError)
		if tx.isWrite {
			tx.UpdateLastWrite()
		}
	}
	tx.commit = nil
}

func (tx *Transaction) Prepare(query string) *sqlite.Stmt {
	if tx.isWrite && tx.commit == nil {
		panic("writes must be inside of a transaction")
	}

	stmt, err := tx.conn.Prepare(query)
	if err != nil {
		panic(fmt.Errorf("failed to prepare statement: %w", err))
	}
	return stmt
}

func (tx Transaction) Finish(stmt *sqlite.Stmt) {
	if err := stmt.Reset(); err != nil {
		panic(fmt.Errorf("failed to reset statement: %w", err))
	}
	if err := stmt.ClearBindings(); err != nil {
		panic(fmt.Errorf("failed to clear bindings: %w", err))
	}
}

func (tx *Transaction) Execute(stmt *sqlite.Stmt) (bool, error) {
	hasRow, err := stmt.Step()
	if err != nil {
		return false, fmt.Errorf("failed to execute statement: %w", err)
	}
	return hasRow, nil
}

// Data Access Layer

var ErrFileNotFound = errors.New("file not found")

func (tx *Transaction) Initialize() {
	tx.MarkAsWrite()
	query := `
		INSERT OR IGNORE INTO settings(
			id,
			site_name,
			login_message,
			default_permissions,
			uses_email,
			uses_invite_code,
			uses_captcha,
			uses_login_captcha,
			captcha_site_key,
			captcha_secret_key
		) VALUES (
			0,
			$site_name,
			$site_message,
			$default_permissions,
			$uses_email,
			$uses_invite_code,
			$uses_captcha,
			$uses_login_captcha,
			$captcha_site_key,
			$captcha_secret_key
	);`

	stmt := tx.Prepare(query)
	defer tx.Finish(stmt)

	stmt.SetText("$site_name", "Clack")
	stmt.SetText("$site_message", "Welcome to Clack!")
	stmt.SetInt64("$default_permissions", PermissionDefault)
	stmt.SetInt64("$uses_email", 0)
	stmt.SetInt64("$uses_invite_code", 0)
	stmt.SetInt64("$uses_captcha", 0)
	stmt.SetInt64("$uses_login_captcha", 0)
	stmt.SetText("$captcha_site_key", "")
	stmt.SetText("$captcha_secret_key", "")

	if _, err := tx.Execute(stmt); err != nil {
		panic(NewError(ErrorCodeInternalError, fmt.Errorf("failed to initialize database: %w", err)))
	}
}

func (tx *Transaction) Checkpoint() {
	tx.MarkAsWrite()
	stmt := tx.Prepare("PRAGMA wal_checkpoint(TRUNCATE);")
	defer tx.Finish(stmt)

	if _, err := tx.Execute(stmt); err != nil {
		panic(NewError(ErrorCodeInternalError, fmt.Errorf("failed to checkpoint database: %w", err)))
	}
}

func (tx *Transaction) QueryUsers(id Snowflake) []User {
	query := `SELECT
			u.id,
			u.user_name,
			u.display_name,
			u.status_message,
			u.profile_message,
			u.profile_color,
			u.avatar_modified,
			u.presence,
			r.role_id
		FROM
			users u
		LEFT JOIN
			user_roles r ON u.id = r.user_id`
	if id != 0 {
		query += ` WHERE u.id = $id`
	}
	stmt := tx.Prepare(query + " ORDER BY u.id;")
	defer tx.Finish(stmt)

	if id != 0 {
		stmt.SetInt64("$id", int64(id))
	}

	users := []User{}

	var currentUser *User = nil
	for hasRow, _ := stmt.Step(); hasRow; hasRow, _ = stmt.Step() {
		user := User{
			ID:             Snowflake(stmt.GetInt64("id")),
			UserName:       stmt.GetText("user_name"),
			DisplayName:    stmt.GetText("display_name"),
			StatusMessage:  stmt.GetText("status_message"),
			ProfileMessage: stmt.GetText("profile_message"),
			ProfileColor:   int(stmt.GetInt64("profile_color")),
			AvatarModified: int(stmt.GetInt64("avatar_modified")),
			Presence:       int(stmt.GetInt64("presence")),
			Roles:          []Snowflake{},
		}

		if !stmt.IsNull("role_id") {
			user.Roles = append(user.Roles, Snowflake(stmt.GetInt64("role_id")))
		}

		if currentUser == nil {
			currentUser = &user
		} else if currentUser.ID == user.ID {
			currentUser.Roles = append(currentUser.Roles, user.Roles...)
		} else {
			users = append(users, *currentUser)
			currentUser = &user
		}
	}

	if currentUser != nil {
		users = append(users, *currentUser)
	}

	return users
}

func (tx *Transaction) GetUser(id Snowflake) (User, error) {
	users := tx.QueryUsers(id)
	if len(users) == 0 {
		return User{}, NewError(ErrorCodeInvalidRequest, fmt.Errorf("user not found", id))
	}
	return users[0], nil
}

func (tx *Transaction) GetAllUsers() []User {
	return tx.QueryUsers(0)
}

func (tx *Transaction) QueryChannels(id Snowflake) []Channel {
	query := `SELECT
			c.id,
			c.type,
			c.name,
			c.description,
			c.position,
			c.parent_id,
			crp.role_id,
			crp.allow AS role_allow,
			crp.deny AS role_deny,
			cup.user_id,
			cup.allow AS user_allow,
			cup.deny AS user_deny
		FROM
			channels c
		LEFT JOIN
			channel_role_permissions crp ON c.id = crp.channel_id
		LEFT JOIN
			channel_user_permissions cup ON c.id = cup.channel_id`
	if id != 0 {
		query += ` WHERE c.id = $id`
	}
	stmt := tx.Prepare(query + ` ORDER BY c.id;`)
	defer tx.Finish(stmt)

	if id != 0 {
		stmt.SetInt64("$id", int64(id))
	}

	channels := []Channel{}
	var currentChannel *Channel = nil
	var currentPermissions map[Snowflake]map[Snowflake]bool = make(map[Snowflake]map[Snowflake]bool)

	for hasRow, _ := stmt.Step(); hasRow; hasRow, _ = stmt.Step() {
		channel := Channel{
			ID:          Snowflake(stmt.GetInt64("id")),
			Type:        int(stmt.GetInt64("type")),
			Name:        stmt.GetText("name"),
			Description: stmt.GetText("description"),
			Position:    int(stmt.GetInt64("position")),
			ParentID:    Snowflake(stmt.GetInt64("parent_id")),
			Overwrites:  []Overwrite{},
		}

		if currentPermissions[channel.ID] == nil {
			currentPermissions[channel.ID] = make(map[Snowflake]bool)
		}

		// Handle role overwrites
		if !stmt.IsNull("role_id") {
			overwrite := Overwrite{
				ID:    Snowflake(stmt.GetInt64("role_id")),
				Type:  OverwriteTypeRole,
				Allow: int(stmt.GetInt64("role_allow")),
				Deny:  int(stmt.GetInt64("role_deny")),
			}
			if !currentPermissions[channel.ID][overwrite.ID] {
				channel.Overwrites = append(channel.Overwrites, overwrite)
				currentPermissions[channel.ID][overwrite.ID] = true
			}
		}

		// Handle user overwrites
		if !stmt.IsNull("user_id") {
			overwrite := Overwrite{
				ID:    Snowflake(stmt.GetInt64("user_id")),
				Type:  OverwriteTypeUser,
				Allow: int(stmt.GetInt64("user_allow")),
				Deny:  int(stmt.GetInt64("user_deny")),
			}
			if !currentPermissions[channel.ID][overwrite.ID] {
				channel.Overwrites = append(channel.Overwrites, overwrite)
				currentPermissions[channel.ID][overwrite.ID] = true
			}
		}

		if currentChannel == nil {
			currentChannel = &channel
		} else if currentChannel.ID == channel.ID {
			currentChannel.Overwrites = append(currentChannel.Overwrites, channel.Overwrites...)
		} else {
			channels = append(channels, *currentChannel)
			currentChannel = &channel
		}
	}

	if currentChannel != nil {
		channels = append(channels, *currentChannel)
	}

	return channels
}

func (tx *Transaction) GetChannel(id Snowflake) (Channel, error) {
	channels := tx.QueryChannels(id)
	if len(channels) == 0 {
		return Channel{}, NewError(ErrorCodeInvalidRequest, fmt.Errorf("channel not found"))
	}
	return channels[0], nil
}

func (tx *Transaction) GetAllChannels() []Channel {
	return tx.QueryChannels(0)
}

func (tx *Transaction) GetChannelByMessage(messageID Snowflake) (Snowflake, error) {
	stmt := tx.Prepare(`
		SELECT
			channel_id
		FROM
			messages
		WHERE
			id = $id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$id", int64(messageID))

	hasRow, err := stmt.Step()
	if err != nil {
		return 0, fmt.Errorf("failed to get channel by message: %w", err)
	}

	if !hasRow {
		return 0, NewError(ErrorCodeInvalidRequest, fmt.Errorf("message not found"))
	}

	channel_id := Snowflake(stmt.GetInt64("channel_id"))

	return channel_id, nil
}

func (tx *Transaction) AddChannel(name string, channelType int, description string, position int, parentID Snowflake) (Snowflake, error) {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		INSERT INTO channels(id, type, name, description, position, parent_id)
		VALUES ($id, $type, $name, $description, $position, $parent_id);`,
	)
	defer tx.Finish(stmt)

	channelID := snowflake.New()

	stmt.SetInt64("$id", int64(channelID))
	stmt.SetInt64("$type", int64(channelType))
	stmt.SetText("$name", name)
	stmt.SetText("$description", description)
	stmt.SetInt64("$position", int64(position))

	if parentID == 0 {
		stmt.SetNull("$parent_id")
	} else {
		stmt.SetInt64("$parent_id", int64(parentID))
	}

	if _, err := tx.Execute(stmt); err != nil {
		return 0, NewError(ErrorCodeInternalError, err)
	}

	return channelID, nil
}

func (tx *Transaction) QueryRoles(id Snowflake) []Role {
	query := `SELECT
			id,
			name,
			color,
			position,
			permissions,
			hoisted,
			mentionable
		FROM
			roles`
	if id != 0 {
		query += ` WHERE id = $id`
	}
	stmt := tx.Prepare(query + ` ORDER BY id;`)
	defer tx.Finish(stmt)

	if id != 0 {
		stmt.SetInt64("$id", int64(id))
	}

	roles := []Role{}

	for hasRow, _ := stmt.Step(); hasRow; hasRow, _ = stmt.Step() {
		role := Role{
			ID:          Snowflake(stmt.GetInt64("id")),
			Name:        stmt.GetText("name"),
			Color:       int(stmt.GetInt64("color")),
			Permissions: int(stmt.GetInt64("permissions")),
			Hoisted:     stmt.GetInt64("hoisted") != 0,
			Mentionable: stmt.GetInt64("mentionable") != 0,
		}
		roles = append(roles, role)
	}

	return roles
}

func (tx *Transaction) GetRole(id Snowflake) (Role, error) {
	roles := tx.QueryRoles(id)
	if len(roles) == 0 {
		return Role{}, NewError(ErrorCodeInvalidRequest, fmt.Errorf("role not found"))
	}
	return roles[0], nil
}

func (tx *Transaction) GetAllRoles() []Role {
	return tx.QueryRoles(0)
}

func (tx *Transaction) AddRole(name string, color int, position int, permissions int, hoisted bool, mentionable bool) (Snowflake, error) {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		INSERT INTO roles(id, name, color, position, permissions, hoisted, mentionable)
		VALUES ($id, $name, $color, $position, $permissions, $hoisted, $mentionable);`,
	)
	defer tx.Finish(stmt)

	roleID := snowflake.New()

	stmt.SetInt64("$id", int64(roleID))
	stmt.SetText("$name", name)
	stmt.SetInt64("$color", int64(color))
	stmt.SetInt64("$position", int64(position))
	stmt.SetInt64("$permissions", int64(permissions))
	stmt.SetBool("$hoisted", hoisted)
	stmt.SetBool("$mentionable", mentionable)

	if _, err := tx.Execute(stmt); err != nil {
		return 0, NewError(ErrorCodeInternalError, err)
	}

	return roleID, nil
}

func (tx *Transaction) UpdateRole(id Snowflake, name string, color int, position int, permissions int, hoisted bool, mentionable bool) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`UPDATE roles
		SET
			name = $name,
			color = $color,
			position = $position,
			permissions = $permissions,
			hoisted = $hoisted,
			mentionable = $mentionable
		WHERE id = $id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$id", int64(id))
	stmt.SetText("$name", name)
	stmt.SetInt64("$color", int64(color))
	stmt.SetInt64("$position", int64(position))
	stmt.SetInt64("$permissions", int64(permissions))
	stmt.SetBool("$hoisted", hoisted)
	stmt.SetBool("$mentionable", mentionable)

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func (tx *Transaction) DeleteRole(id Snowflake) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`DELETE FROM roles WHERE id = $id;`)
	defer tx.Finish(stmt)

	stmt.SetInt64("$id", int64(id))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func (tx *Transaction) AddRoleToUser(userID Snowflake, roleID Snowflake) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`INSERT INTO user_roles(user_id, role_id) VALUES ($user_id, $role_id);`)
	defer tx.Finish(stmt)

	stmt.SetInt64("$user_id", int64(userID))
	stmt.SetInt64("$role_id", int64(roleID))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func (tx *Transaction) RemoveRoleFromUser(userID Snowflake, roleID Snowflake) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`DELETE FROM user_roles WHERE user_id = $user_id AND role_id = $role_id;`)
	defer tx.Finish(stmt)

	stmt.SetInt64("$user_id", int64(userID))
	stmt.SetInt64("$role_id", int64(roleID))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}
func (tx *Transaction) QueryEmojis(id Snowflake) []Emoji {
	query := `SELECT
			name,
			id
		FROM
			emojis`
	if id != 0 {
		query += ` WHERE id = $id`
	}
	stmt := tx.Prepare(query + ` ORDER BY name;`)
	defer tx.Finish(stmt)

	emojis := []Emoji{}

	for hasRow, _ := stmt.Step(); hasRow; hasRow, _ = stmt.Step() {
		emoji := Emoji{
			Name: stmt.GetText("name"),
			ID:   Snowflake(stmt.GetInt64("id")),
		}
		emojis = append(emojis, emoji)
	}

	return emojis
}

func (tx *Transaction) GetEmoji(id Snowflake) (Emoji, error) {
	emjs := tx.QueryEmojis(id)
	if len(emjs) == 0 {
		return Emoji{}, NewError(ErrorCodeInvalidRequest, fmt.Errorf("emoji with ID %d not found", id))
	}
	return emjs[0], nil
}

func (tx *Transaction) GetAllEmojis() []Emoji {
	return tx.QueryEmojis(0)
}

func (tx *Transaction) ValidateEmoji(emojiID Snowflake) bool {
	if emoji.IsUnicodeEmojiID(int64(emojiID)) {
		return true
	} else {
		if _, err := tx.GetEmoji(emojiID); err == nil {
			return true
		} else {
			return false
		}
	}
}

func (tx *Transaction) UseToken(token string) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		UPDATE user_tokens
		SET last_used_at = $last_used_at
		WHERE token = $token;`,
	)
	defer tx.Finish(stmt)

	now := time.Now().UnixMilli()
	stmt.SetText("$token", token)
	stmt.SetInt64("$last_used_at", now)
	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func (tx *Transaction) Authenticate(token string) (Snowflake, error) {
	stmt := tx.Prepare(`
		SELECT
			user_id
		FROM
			user_tokens
		WHERE
			token = $token;`,
	)
	defer tx.Finish(stmt)

	stmt.SetText("$token", token)

	hasRow, err := stmt.Step()
	if err != nil {
		return 0, NewError(ErrorCodeInternalError, err)
	}

	if !hasRow {
		return 0, NewError(ErrorCodeInvalidToken, nil)
	}

	if err := tx.UseToken(token); err != nil {
		return 0, err
	}

	userID := Snowflake(stmt.GetInt64("user_id"))
	return userID, nil
}

func (tx *Transaction) AddToken(userID Snowflake) (string, error) {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		INSERT INTO user_tokens(user_id, token, created_at, last_used_at)
		VALUES ($user_id, $token, $created_at, $last_used_at);`,
	)
	defer tx.Finish(stmt)

	token := GetRandom256()
	now := time.Now().UnixMilli()

	stmt.SetInt64("$user_id", int64(userID))
	stmt.SetText("$token", token)
	stmt.SetInt64("$created_at", now)
	stmt.SetInt64("$last_used_at", now)

	if _, err := tx.Execute(stmt); err != nil {
		return "", NewError(ErrorCodeInternalError, err)
	}

	return token, nil
}

func (tx *Transaction) Login(username, password string) (Snowflake, string, error) {
	stmt := tx.Prepare(`
		SELECT
			id,
			hash,
			salt
		FROM
			users
		WHERE
			user_name = $user_name`,
	)
	defer tx.Finish(stmt)

	stmt.SetText("$user_name", username)

	hasRow, err := stmt.Step()
	if err != nil {
		return 0, "", NewError(ErrorCodeInternalError, err)
	}

	if !hasRow {
		return 0, "", NewError(ErrorCodeInvalidCredentials, fmt.Errorf("user not found"))
	}

	salt := stmt.GetText("salt")
	hash := stmt.GetText("hash")
	userID := Snowflake(stmt.GetInt64("id"))

	if hash != HashSha256(password, salt) {
		return 0, "", NewError(ErrorCodeInvalidCredentials, fmt.Errorf("invalid password"))
	}

	if token, err := tx.AddToken(userID); err != nil {
		return 0, "", err
	} else {
		return userID, token, nil
	}
}

func (tx *Transaction) IsUsernameValid(username string) (bool, error) {
	if username == "" || len(username) < 3 || len(username) > 32 {
		return false, NewError(ErrorCodeInvalidUsername, fmt.Errorf("username '%s' must be between 3 and 32 characters long", username))
	}

	stmt := tx.Prepare(`
		SELECT
			user_name
		FROM
			users
		WHERE
			user_name = $user_name;`,
	)
	defer tx.Finish(stmt)

	stmt.SetText("$user_name", username)

	found, err := tx.Execute(stmt)

	if err != nil {
		return false, NewError(ErrorCodeInternalError, err)
	}

	if found {
		return false, NewError(ErrorCodeTakenUsername, fmt.Errorf("username '%s' is already taken", username))
	}

	return true, nil
}

func (tx *Transaction) Register(username, password, email, inviteCode string) (User, string, error) {
	tx.MarkAsWrite()

	salt := GetRandom128()
	hash := HashSha256(password, salt)

	userID, err := tx.AddUser(username, hash, salt, inviteCode, email)
	if err != nil {
		return User{}, "", err
	}

	displayName := cases.Title(language.English, cases.NoLower).String(username)
	err = tx.SetUserProfile(userID, displayName, "", "", ProfileColorDefault, AvatarModifiedDefault)
	if err != nil {
		return User{}, "", NewError(ErrorCodeInternalError, fmt.Errorf("failed to set user profile: %w", err))
	}

	token, err := tx.AddToken(userID)
	if err != nil {
		return User{}, "", err
	}

	user, err := tx.GetUser(userID)
	if err != nil {
		return User{}, "", NewError(ErrorCodeInternalError, fmt.Errorf("failed to retrieve user after registration: %w", err))
	}

	return user, token, nil
}

func (tx *Transaction) AddUser(userName, hash, salt, inviteCode, email string) (Snowflake, error) {
	if _, err := tx.IsUsernameValid(userName); err != nil {
		return 0, err
	}

	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		INSERT INTO users(id, user_name, display_name, hash, salt, invite_code, email, presence)
		VALUES ($id, $user_name, $display_name, $hash, $salt, $invite_code, $email, $presence);`,
	)
	defer tx.Finish(stmt)

	userID := snowflake.New()
	stmt.SetInt64("$id", int64(userID))
	stmt.SetText("$user_name", userName)
	stmt.SetText("$display_name", userName)
	stmt.SetText("$hash", hash)
	stmt.SetText("$salt", salt)
	stmt.SetInt64("$presence", int64(UserPresenceOffline))

	if inviteCode != "" {
		stmt.SetText("$invite_code", inviteCode)
	} else {
		stmt.SetNull("$invite_code")
	}

	if email != "" {
		stmt.SetText("$email", email)
	} else {
		stmt.SetNull("$email")
	}

	if _, err := tx.Execute(stmt); err != nil {
		return 0, NewError(ErrorCodeInternalError, fmt.Errorf("failed to add user: %w", err))
	}

	return userID, nil
}

func (tx *Transaction) SetUserProfile(userID Snowflake, displayName, statusMessage, profileMessage string, profileColor, avatarModified int) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		UPDATE users
		SET
			display_name = $display_name,
			status_message = $status_message,
			profile_message = $profile_message,
			profile_color = $profile_color,
			avatar_modified = $avatar_modified
		WHERE id = $id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$id", int64(userID))
	stmt.SetText("$display_name", displayName)
	stmt.SetText("$status_message", statusMessage)
	stmt.SetText("$profile_message", profileMessage)
	stmt.SetInt64("$profile_color", int64(profileColor))
	stmt.SetInt64("$avatar_modified", int64(avatarModified))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func (tx *Transaction) SetUserPresence(userID Snowflake, presence int) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		UPDATE users
		SET
			presence = $presence
		WHERE id = $id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$id", int64(userID))
	stmt.SetInt64("$presence", int64(presence))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func (tx *Transaction) GetSettings() (Settings, error) {
	stmt := tx.Prepare(`
		SELECT
			site_name,
			login_message,
			default_permissions,
			uses_email,
			uses_invite_code,
			uses_captcha,
			uses_login_captcha,
			captcha_site_key,
			captcha_secret_key
		FROM
			settings
		WHERE id = 0;`,
	)
	defer tx.Finish(stmt)

	hasRow, err := stmt.Step()
	if err != nil {
		return Settings{}, NewError(ErrorCodeInternalError, err)
	}

	if !hasRow {
		return Settings{}, NewError(ErrorCodeInternalError, fmt.Errorf("settings not found"))
	}

	settings := Settings{
		SiteName:           stmt.GetText("site_name"),
		LoginMessage:       stmt.GetText("login_message"),
		DefaultPermissions: int(stmt.GetInt64("default_permissions")),
		UsesEmail:          stmt.GetInt64("uses_email") != 0,
		UsesInviteCodes:    stmt.GetInt64("uses_invite_code") != 0,
		UsesCaptcha:        stmt.GetInt64("uses_captcha") != 0,
		UsesLoginCaptcha:   stmt.GetInt64("uses_login_captcha") != 0,
		CaptchaSiteKey:     stmt.GetText("captcha_site_key"),
		CaptchaSecretKey:   stmt.GetText("captcha_secret_key"),
	}

	return settings, nil
}

func (tx *Transaction) SetSettings(settings Settings) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		UPDATE settings
		SET
			site_name = $site_name,
			login_message = $login_message,
			default_permissions = $default_permissions,
			uses_email = $uses_email,
			uses_invite_code = $uses_invite_code,
			uses_captcha = $uses_captcha,
			uses_login_captcha = $uses_login_captcha,
			captcha_site_key = $captcha_site_key,
			captcha_secret_key = $captcha_secret_key
		WHERE id = 0;`,
	)
	defer tx.Finish(stmt)

	stmt.SetText("$site_name", settings.SiteName)
	stmt.SetText("$login_message", settings.LoginMessage)
	stmt.SetInt64("$default_permissions", int64(settings.DefaultPermissions))
	stmt.SetInt64("$uses_email", int64(BoolToInt(settings.UsesEmail)))
	stmt.SetInt64("$uses_invite_code", int64(BoolToInt(settings.UsesInviteCodes)))
	stmt.SetInt64("$uses_captcha", int64(BoolToInt(settings.UsesCaptcha)))
	stmt.SetInt64("$uses_login_captcha", int64(BoolToInt(settings.UsesLoginCaptcha)))
	stmt.SetText("$captcha_site_key", settings.CaptchaSiteKey)
	stmt.SetText("$captcha_secret_key", settings.CaptchaSecretKey)

	_, err := tx.Execute(stmt)
	if err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

//go:embed sql/message_query.sql
var message_query_string string

// TODO: Maybe split into multiple queries.
func (tx *Transaction) GetMessagesByAnchor(channelID Snowflake, anchorID Snowflake, limit int, before bool) ([]Message, error) {
	// Start with the base query from the embedded SQL file
	baseQuery := strings.TrimSuffix(message_query_string, ";")

	var finalQuery string
	var params []int64

	// Build the WHERE clause based on the anchorID and the 'before' flag
	if anchorID != 0 {
		if before {
			finalQuery = baseQuery + `
				WHERE m.channel_id = ? AND m.id < ?
				ORDER BY m.id DESC
				LIMIT ?;`
			params = append(params, int64(channelID), int64(anchorID), int64(limit))
		} else {
			finalQuery = baseQuery + `
				WHERE m.channel_id = ? AND m.id > ?
				ORDER BY m.id ASC
				LIMIT ?;`
			params = append(params, int64(channelID), int64(anchorID), int64(limit))
		}
	} else {
		// If no anchorID is provided, fetch the most recent messages
		finalQuery = baseQuery + `
			WHERE m.channel_id = ?
			ORDER BY m.id DESC
			LIMIT ?;`
		params = append(params, int64(channelID), int64(limit))
	}

	stmt := tx.Prepare(finalQuery)
	defer tx.Finish(stmt)

	for i, param := range params {
		stmt.BindInt64(i+1, param)
	}

	messages, err := tx.QueryMessages(stmt)
	if err != nil {
		return nil, err
	}

	// If fetching messages after the anchor, they were ordered ascending; reverse to maintain consistency
	if anchorID == 0 || before {
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}

	return messages, nil
}

func (tx *Transaction) GetMessages(ids []Snowflake, required bool) ([]Message, error) {
	baseQuery := strings.TrimSuffix(message_query_string, ";")
	query := baseQuery + ` WHERE m.id = $id;`

	stmt := tx.Prepare(query)
	defer tx.Finish(stmt)

	messages := make([]Message, 0, len(ids))

	for _, id := range ids {
		stmt.SetInt64("$id", int64(id))

		parsed, err := tx.QueryMessages(stmt)
		stmt.Reset()

		if err != nil {
			return nil, err
		}

		if len(parsed) == 0 {
			if required {
				return nil, fmt.Errorf("message with ID %d not found", id)
			} else {
				continue
			}
		}
		messages = append(messages, parsed[0])
	}
	return messages, nil
}

func (tx *Transaction) GetMessage(id Snowflake) (Message, error) {
	messages, err := tx.GetMessages([]Snowflake{id}, true)
	if err != nil || len(messages) == 0 {
		return Message{}, err
	}
	return messages[0], nil
}

func (tx *Transaction) QueryMessages(stmt *sqlite.Stmt) ([]Message, error) {
	messages := []Message{}

	// Iterate over the result rows
	for hasRow, stepErr := stmt.Step(); hasRow; hasRow, stepErr = stmt.Step() {
		if stepErr != nil {
			return nil, fmt.Errorf("error stepping through messages: %w", stepErr)
		}

		// Extract basic message fields
		message := Message{
			ID:              Snowflake(stmt.GetInt64("id")),
			Type:            int(stmt.GetInt64("type")),
			ChannelID:       Snowflake(stmt.GetInt64("channel_id")),
			Timestamp:       int(stmt.GetInt64("timestamp")),
			Pinned:          stmt.GetInt64("pinned") != 0,
			AuthorID:        Snowflake(stmt.GetInt64("author_id")),
			ReferenceID:     Snowflake(stmt.GetInt64("reference_id")),
			Content:         stmt.GetText("content"),
			EditedTimestamp: int(stmt.GetInt64("edited_timestamp")),
		}

		// Parse Attachments JSON
		attachmentsJSON := stmt.GetText("attachments")
		if err := json.Unmarshal([]byte(attachmentsJSON), &message.Attachments); err != nil {
			return nil, fmt.Errorf("failed to unmarshal attachments: %w", err)
		}

		// Parse Embeds JSON
		embedsJSON := stmt.GetText("embeds")
		if err := json.Unmarshal([]byte(embedsJSON), &message.Embeds); err != nil {
			return nil, fmt.Errorf("failed to unmarshal embeds: %w", err)
		}

		// Parse Reactions JSON
		reactionsJSON := stmt.GetText("reactions")
		if err := json.Unmarshal([]byte(reactionsJSON), &message.Reactions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal reactions: %w", err)
		}

		// Parse Mentioned Users JSON
		mentionedUsersJSON := stmt.GetText("mentioned_users")
		if err := json.Unmarshal([]byte(mentionedUsersJSON), &message.MentionedUsers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal mentioned_users: %w", err)
		}

		// Parse Mentioned Roles JSON
		mentionedRolesJSON := stmt.GetText("mentioned_roles")
		if err := json.Unmarshal([]byte(mentionedRolesJSON), &message.MentionedRoles); err != nil {
			return nil, fmt.Errorf("failed to unmarshal mentioned_roles: %w", err)
		}

		// Parse Mentioned Channels JSON
		mentionedChannelsJSON := stmt.GetText("mentioned_channels")
		if err := json.Unmarshal([]byte(mentionedChannelsJSON), &message.MentionedChannels); err != nil {
			return nil, fmt.Errorf("failed to unmarshal mentioned_channels: %w", err)
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func (tx *Transaction) AddPreviews(id Snowflake, width, height int, preload string) error {
	tx.MarkAsWrite()

	preview_stmt := tx.Prepare(`
		INSERT INTO previews(id, width, height, preload)
		VALUES ($id, $width, $height, $preload);`,
	)
	defer tx.Finish(preview_stmt)

	preview_stmt.SetInt64("$id", int64(id))
	preview_stmt.SetInt64("$width", int64(width))
	preview_stmt.SetInt64("$height", int64(height))
	preview_stmt.SetText("$preload", preload)

	if _, err := tx.Execute(preview_stmt); err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert previews: %w", err))
	}

	return nil
}

func (tx *Transaction) AddAttachment(messageID Snowflake, attachment *Attachment) error {
	tx.MarkAsWrite()
	attach_stmt := tx.Prepare(`
		INSERT INTO attachments(id, message_id, type, mimetype, filename, size)
		VALUES ($id, $message_id, $type, $mimetype, $filename, $size);`,
	)

	attach_stmt.SetInt64("$id", int64(attachment.ID))
	attach_stmt.SetInt64("$message_id", int64(messageID))
	attach_stmt.SetInt64("$type", int64(attachment.Type))
	attach_stmt.SetText("$mimetype", attachment.MimeType)
	attach_stmt.SetText("$filename", attachment.Filename)
	attach_stmt.SetInt64("$size", int64(attachment.Size))

	if _, err := tx.Execute(attach_stmt); err != nil {
		tx.Finish(attach_stmt)
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert attachment: %w", err))
	}

	tx.Finish(attach_stmt)

	if attachment.Preload != "" {
		if err := tx.AddPreviews(attachment.ID, attachment.Width, attachment.Height, attachment.Preload); err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert attachment preview: %w", err))
		}
	}

	return nil
}

func (tx *Transaction) GetAttachment(messageID Snowflake, attachmentID Snowflake, filename string) (Attachment, error) {
	stmt := tx.Prepare(`
		SELECT
			a.id,
			a.type,
			a.mimetype,
			a.filename,
			p.width,
			p.height,
			p.preload
		FROM
			attachments a
		LEFT JOIN
			previews p ON a.id = p.id
		WHERE
			a.message_id = $message_id AND a.id = $attachment_id AND a.filename = $filename;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$message_id", int64(messageID))
	stmt.SetInt64("$attachment_id", int64(attachmentID))
	stmt.SetText("$filename", filename)

	hasRow, err := stmt.Step()
	if err != nil {
		return Attachment{}, NewError(ErrorCodeInternalError, fmt.Errorf("failed to get attachment: %w", err))
	}

	if !hasRow {
		return Attachment{}, NewError(ErrorCodeInvalidRequest, fmt.Errorf("attachment not found"))
	}

	attachment := Attachment{
		ID:       Snowflake(stmt.GetInt64("id")),
		Type:     int(stmt.GetInt64("type")),
		MimeType: stmt.GetText("mimetype"),
		Filename: stmt.GetText("filename"),
	}

	if !stmt.IsNull("width") {
		attachment.Width = int(stmt.GetInt64("width"))
	}
	if !stmt.IsNull("height") {
		attachment.Height = int(stmt.GetInt64("height"))
	}
	if !stmt.IsNull("preload") {
		attachment.Preload = stmt.GetText("preload")
	}

	return attachment, nil
}

func (tx *Transaction) AddEmbed(messageID Snowflake, embed *Embed) error {
	tx.MarkAsWrite()
	// Assign a new ID to the embed if not already set
	if embed.ID == 0 {
		embed.ID = snowflake.New()
	}

	// Prepare the INSERT statement for the embeds table
	embed_stmt := tx.Prepare(`
	INSERT INTO embeds (
		id,
		message_id,
		type,
		url,
		title,
		description,
		color,
		timestamp,
		image_url,
		image_id,
		thumbnail_url,
		thumbnail_id,
		video_url,
		video_id,
		author_name,
		author_url,
		author_icon_url,
		author_icon_id,
		provider_name,
		provider_url,
		footer_text,
		footer_icon_url,
		footer_icon_id
	) VALUES (
		$id,
		$message_id,
		$type,
		$url,
		$title,
		$description,
		$color,
		$timestamp,
		$image_url,
		$image_id,
		$thumbnail_url,
		$thumbnail_id,
		$video_url,
		$video_id,
		$author_name,
		$author_url,
		$author_icon_url,
		$author_icon_id,
		$provider_name,
		$provider_url,
		$footer_text,
		$footer_icon_url,
		$footer_icon_id
	);`)

	// Bind the embed values
	embed_stmt.SetInt64("$id", int64(embed.ID))
	embed_stmt.SetInt64("$message_id", int64(messageID))
	embed_stmt.SetInt64("$type", int64(embed.Type))
	embed_stmt.SetText("$url", embed.URL)
	embed_stmt.SetText("$title", embed.Title)
	embed_stmt.SetText("$description", embed.Description)
	embed_stmt.SetInt64("$color", int64(embed.Color))
	embed_stmt.SetInt64("$timestamp", int64(embed.Timestamp))

	// Handle Image Media
	if embed.Image != nil {
		embed_stmt.SetText("$image_url", embed.Image.URL)
		embed_stmt.SetInt64("$image_id", int64(embed.Image.ID))
	}

	// Handle Thumbnail Media
	if embed.Thumbnail != nil {
		embed_stmt.SetText("$thumbnail_url", embed.Thumbnail.URL)
		embed_stmt.SetInt64("$thumbnail_id", int64(embed.Thumbnail.ID))
	}

	// Handle Video Media
	if embed.Video != nil {
		embed_stmt.SetText("$video_url", embed.Video.URL)
		embed_stmt.SetInt64("$video_id", int64(embed.Video.ID))
	}

	// Handle Author Information
	if embed.Author != nil {
		embed_stmt.SetText("$author_name", embed.Author.Name)
		embed_stmt.SetText("$author_url", embed.Author.URL)
		if embed.Author.Icon != nil {
			embed_stmt.SetText("$author_icon_url", embed.Author.Icon.URL)
			embed_stmt.SetInt64("$author_icon_id", int64(embed.Author.Icon.ID))
		}
	}

	// Handle Provider Information
	if embed.Provider != nil {
		embed_stmt.SetText("$provider_name", embed.Provider.Name)
		embed_stmt.SetText("$provider_url", embed.Provider.URL)
	}

	// Handle Footer Information
	if embed.Footer != nil {
		embed_stmt.SetText("$footer_text", embed.Footer.Text)
		embed_stmt.SetText("$footer_icon_url", embed.Footer.Icon.URL)
		if embed.Footer.Icon != nil {
			embed_stmt.SetInt64("$footer_icon_id", int64(embed.Footer.Icon.ID))
		}
	}

	// Execute the embed insertion
	_, err := tx.Execute(embed_stmt)
	tx.Finish(embed_stmt)
	if err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert embed: %w", err))
	}

	// Insert embed fields into the embed_fields table
	for _, field := range embed.Fields {
		field_stmt := tx.Prepare(`
		INSERT INTO embed_fields (
			embed_id,
			name,
			value,
			inline
		) VALUES (
			$embed_id,
			$name,
			$value,
			$inline
		);`)

		field_stmt.SetInt64("$embed_id", int64(embed.ID))
		field_stmt.SetText("$name", field.Name)
		field_stmt.SetText("$value", field.Value)
		if field.Inline {
			field_stmt.SetInt64("$inline", 1)
		} else {
			field_stmt.SetInt64("$inline", 0)
		}

		_, err := tx.Execute(field_stmt)
		tx.Finish(field_stmt)

		if err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert embed field '%s': %w", field.Name, err))
		}
	}

	// Add previews for the embed if they exist
	if embed.Image != nil {
		if err := tx.AddPreviews(embed.Image.ID, embed.Image.Width, embed.Image.Height, embed.Image.Preload); err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert embed image preview: %w", err))
		}
	}

	if embed.Thumbnail != nil {
		if err := tx.AddPreviews(embed.Thumbnail.ID, embed.Thumbnail.Width, embed.Thumbnail.Height, embed.Thumbnail.Preload); err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert embed thumbnail preview: %w", err))
		}
	}

	if embed.Video != nil {
		fmt.Println("Adding embed video preview:", embed.Video.Width, embed.Video.Height, embed.Video.Preload)
		if err := tx.AddPreviews(embed.Video.ID, embed.Video.Width, embed.Video.Height, embed.Video.Preload); err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert embed video preview: %w", err))
		}
	}

	return nil
}

func (tx *Transaction) DeleteEmbeds(embedIDs []Snowflake) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		DELETE FROM embeds
		WHERE id = $id;`,
	)
	defer tx.Finish(stmt)

	for _, embedID := range embedIDs {
		stmt.SetInt64("$id", int64(embedID))
		if _, err := tx.Execute(stmt); err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to delete embed %d: %w", embedID, err))
		}
	}

	return nil
}

func (tx *Transaction) AddMessage(message *Message) error {
	tx.MarkAsWrite()
	messages_stmt := tx.Prepare("INSERT OR REPLACE INTO messages (id, type, channel_id, timestamp, author_id, reference_id, content) VALUES ($id, $type, $channel_id, $timestamp, $author_id, $reference_id, $content);")
	/*defer func() {
		if r := recover(); r != nil {
			var err error = r.(error)
			commit(&err)
			panic(r)
		}
	}()*/

	messages_stmt.SetInt64("$id", int64(message.ID))
	messages_stmt.SetInt64("$type", int64(message.Type))
	messages_stmt.SetInt64("$channel_id", int64(message.ChannelID))
	messages_stmt.SetInt64("$timestamp", int64(message.Timestamp))
	messages_stmt.SetInt64("$author_id", int64(message.AuthorID))
	messages_stmt.SetText("$content", message.Content)

	if message.ReferenceID != 0 {
		messages_stmt.SetInt64("$reference_id", int64(message.ReferenceID))
	} else {
		messages_stmt.SetNull("$reference_id")
	}

	_, err := tx.Execute(messages_stmt)
	tx.Finish(messages_stmt)

	if err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to insert message: %w", err))
	}

	tx.SetMessageMentions(message.ID, message.MentionedUsers, message.MentionedRoles, message.MentionedChannels)

	for _, embed := range message.Embeds {
		if err := tx.AddEmbed(message.ID, &embed); err != nil {
			return err
		}
	}

	for _, attachment := range message.Attachments {
		if err := tx.AddAttachment(message.ID, &attachment); err != nil {
			return err
		}
	}

	return nil
}

func (tx *Transaction) SetMessage(id Snowflake, content string, mentionedUsers []Snowflake, mentionedRoles []Snowflake, mentionedChannels []Snowflake, deletedEmbeds []Snowflake) error {
	tx.MarkAsWrite()
	if err := tx.DeleteEmbeds(deletedEmbeds); err != nil {
		return err
	}

	if err := tx.SetMessageContent(id, content); err != nil {
		return err
	}

	if err := tx.SetMessageMentions(id, mentionedUsers, mentionedRoles, mentionedChannels); err != nil {
		return err
	}
	return nil
}

func (tx *Transaction) SetMessageContent(id Snowflake, content string) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		UPDATE messages
		SET content = $content, edited_timestamp = $edited_timestamp
		WHERE id = $id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetText("$content", content)
	stmt.SetInt64("$edited_timestamp", time.Now().UnixMilli())
	stmt.SetInt64("$id", int64(id))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to set message: %w", err))
	}

	return nil
}

func (tx *Transaction) SetMessageMentions(id Snowflake, mentionedUsers []Snowflake, mentionedRoles []Snowflake, mentionedChannels []Snowflake) error {
	tx.MarkAsWrite()
	user_delete_stmt := tx.Prepare(`DELETE FROM message_user_mentions WHERE message_id = $message_id;`)
	user_delete_stmt.SetInt64("$message_id", int64(id))
	_, err := tx.Execute(user_delete_stmt)
	tx.Finish(user_delete_stmt)
	if err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to clear user mentions: %w", err))
	}

	role_delete_stmt := tx.Prepare(`DELETE FROM message_role_mentions WHERE message_id = $message_id;`)
	role_delete_stmt.SetInt64("$message_id", int64(id))
	_, err = tx.Execute(role_delete_stmt)
	tx.Finish(role_delete_stmt)
	if err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to clear role mentions: %w", err))
	}

	channel_delete_stmt := tx.Prepare(`DELETE FROM message_channel_mentions WHERE message_id = $message_id;`)
	channel_delete_stmt.SetInt64("$message_id", int64(id))
	_, err = tx.Execute(channel_delete_stmt)
	tx.Finish(channel_delete_stmt)
	if err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to clear channel mentions: %w", err))
	}

	user_insert_stmt := tx.Prepare("INSERT INTO message_user_mentions (message_id, user_id) VALUES ($message_id, $user_id);")
	for _, mentionedUserID := range mentionedUsers {
		user_insert_stmt.SetInt64("$message_id", int64(id))
		user_insert_stmt.SetInt64("$user_id", int64(mentionedUserID))
		_, err := tx.Execute(user_insert_stmt)
		tx.Finish(user_insert_stmt)
		if err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to add user mention: %w", err))
		}
	}
	tx.Finish(user_insert_stmt)

	role_insert_stmt := tx.Prepare("INSERT INTO message_role_mentions (message_id, role_id) VALUES ($message_id, $role_id);")
	for _, mentionedRoleID := range mentionedRoles {
		role_insert_stmt.SetInt64("$message_id", int64(id))
		role_insert_stmt.SetInt64("$role_id", int64(mentionedRoleID))
		_, err := tx.Execute(role_insert_stmt)
		tx.Finish(role_insert_stmt)
		if err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to add role mention: %w", err))
		}
	}
	tx.Finish(role_insert_stmt)

	channel_insert_stmt := tx.Prepare("INSERT INTO message_channel_mentions (message_id, channel_id) VALUES ($message_id, $channel_id);")
	for _, mentionedChannelID := range mentionedChannels {
		channel_insert_stmt.SetInt64("$message_id", int64(id))
		channel_insert_stmt.SetInt64("$channel_id", int64(mentionedChannelID))
		_, err := tx.Execute(channel_insert_stmt)
		tx.Finish(channel_insert_stmt)
		if err != nil {
			return NewError(ErrorCodeInternalError, fmt.Errorf("failed to add channel mention: %w", err))
		}
	}
	tx.Finish(channel_insert_stmt)

	return nil
}

func (tx *Transaction) DeleteMessage(id Snowflake) error {
	tx.MarkAsWrite()
	stmt := tx.Prepare(`
		DELETE FROM messages
		WHERE id = $id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$id", int64(id))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to delete message: %w", err))
	}

	return nil
}

func (tx *Transaction) AddReaction(messageID Snowflake, userID Snowflake, emojiID Snowflake) error {
	tx.MarkAsWrite()
	if !tx.ValidateEmoji(emojiID) {
		return NewError(ErrorCodeInvalidRequest, fmt.Errorf("emoji not found"))
	}

	stmt := tx.Prepare(`
		INSERT INTO reactions (message_id, user_id, emoji_id)
		VALUES ($message_id, $user_id, $emoji_id);`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$message_id", int64(messageID))
	stmt.SetInt64("$user_id", int64(userID))
	stmt.SetInt64("$emoji_id", int64(emojiID))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to add reaction: %w", err))
	}

	return nil
}

func (tx *Transaction) DeleteReaction(messageID Snowflake, userID Snowflake, emojiID Snowflake) error {
	tx.MarkAsWrite()
	if !tx.ValidateEmoji(emojiID) {
		return NewError(ErrorCodeInvalidRequest, fmt.Errorf("emoji not found"))
	}

	stmt := tx.Prepare(`
		DELETE FROM reactions
		WHERE message_id = $message_id AND user_id = $user_id AND emoji_id = $emoji_id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$message_id", int64(messageID))
	stmt.SetInt64("$user_id", int64(userID))
	stmt.SetInt64("$emoji_id", int64(emojiID))

	if _, err := tx.Execute(stmt); err != nil {
		return NewError(ErrorCodeInternalError, fmt.Errorf("failed to delete reaction: %w", err))
	}

	return nil
}

func (tx *Transaction) GetReactionCount(messageID Snowflake, emojiID Snowflake) (int, error) {
	if !tx.ValidateEmoji(emojiID) {
		return 0, NewError(ErrorCodeInvalidRequest, fmt.Errorf("emoji not found"))
	}

	stmt := tx.Prepare(`
		SELECT COUNT(*) AS count
		FROM reactions
		WHERE message_id = $message_id AND emoji_id = $emoji_id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$message_id", int64(messageID))
	stmt.SetInt64("$emoji_id", int64(emojiID))

	hasRow, err := stmt.Step()
	if err != nil {
		return 0, NewError(ErrorCodeInternalError, fmt.Errorf("failed to get reaction count: %w", err))
	}

	if !hasRow {
		return 0, nil
	}

	count := int(stmt.GetInt64("count"))
	return count, nil
}

func (tx *Transaction) GetReactionUsers(messageID Snowflake, emojiID Snowflake) ([]Snowflake, error) {
	if !tx.ValidateEmoji(emojiID) {
		return nil, NewError(ErrorCodeInvalidRequest, fmt.Errorf("emoji not found"))
	}

	stmt := tx.Prepare(`
		SELECT user_id
		FROM reactions
		WHERE message_id = $message_id AND emoji_id = $emoji_id;`,
	)
	defer tx.Finish(stmt)

	stmt.SetInt64("$message_id", int64(messageID))
	stmt.SetInt64("$emoji_id", int64(emojiID))

	users := []Snowflake{}

	for hasRow, stepErr := stmt.Step(); hasRow; hasRow, stepErr = stmt.Step() {
		if stepErr != nil {
			return nil, NewError(ErrorCodeInternalError, fmt.Errorf("failed to get reaction users: %w", stepErr))
		}
		users = append(users, Snowflake(stmt.GetInt64("user_id")))
	}

	return users, nil
}

func (tx *Transaction) IsURLAllowed(embedID Snowflake, requestedURL string) (bool, error) {
	stmt := tx.Prepare(`
		SELECT
			image_url,
			thumbnail_url,
			video_url,
			author_icon_url,
			footer_icon_url
		FROM
			embeds
		WHERE
			id = $embed_id;`)
	defer tx.Finish(stmt)

	stmt.SetInt64("$embed_id", int64(embedID))

	found, err := tx.Execute(stmt)

	if err != nil {
		return false, fmt.Errorf("failed to execute query: %w", err)
	}

	if !found {
		return false, ErrFileNotFound // Or a more specific error like ErrEmbedNotFound
	}

	imageURL := stmt.GetText("image_url")
	thumbnailURL := stmt.GetText("thumbnail_url")
	videoURL := stmt.GetText("video_url")
	authorIconURL := stmt.GetText("author_icon_url")
	footerIconURL := stmt.GetText("footer_icon_url")

	if (imageURL == requestedURL) ||
		(thumbnailURL == requestedURL) ||
		(videoURL == requestedURL) ||
		(authorIconURL == requestedURL) ||
		(footerIconURL == requestedURL) {
		return true, nil
	}

	return false, nil
}

func (tx *Transaction) GetPermissionsByUser(userID Snowflake) (int, *User) {
	var allow int = 0
	var settings Settings
	var user User
	var err error

	if settings, err = tx.GetSettings(); err != nil {
		return 0, nil
	}

	allow = settings.DefaultPermissions

	if user, err = tx.GetUser(userID); err != nil {
		return 0, &user
	}

	for _, roleID := range user.Roles {
		if role, err := tx.GetRole(roleID); err == nil {
			allow |= role.Permissions
		}
	}

	if (allow & PermissionAdministrator) != 0 {
		return PermissionAll, &user
	}
	return allow, &user
}

func (tx *Transaction) GetPermissionsByMessage(userID Snowflake, messageID Snowflake) int {
	channelID, err := tx.GetChannelByMessage(messageID)

	if err != nil {
		return 0
	}

	return tx.GetPermissionsByChannel(userID, channelID)
}

func (tx *Transaction) GetPermissionsByChannel(userID Snowflake, channelID Snowflake) int {
	var allow int = 0
	var deny int = 0
	var user *User

	allow, user = tx.GetPermissionsByUser(userID)

	if channelID != 0 {
		if channel, err := tx.GetChannel(channelID); err == nil {
			for _, overwrite := range channel.Overwrites {
				if overwrite.Type == OverwriteTypeRole {
					for _, roleID := range user.Roles {
						if overwrite.ID == roleID {
							allow |= overwrite.Allow
							deny |= overwrite.Deny
							break
						}
					}
				} else if overwrite.Type == OverwriteTypeUser {
					if overwrite.ID == user.ID {
						allow |= overwrite.Allow
						deny |= overwrite.Deny
					}
				}
			}
		}
	}

	permissions := allow & (^deny)

	if (permissions & PermissionAdministrator) != 0 {
		return PermissionAll
	}

	return permissions
}
