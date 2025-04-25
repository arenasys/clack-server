package storage

import (
	. "clack/common"
	"clack/common/snowflake"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Data Access Layer

var ErrFileNotFound = errors.New("file not found")

func ExecuteStatement(stmt *sqlite.Stmt) (bool, error) {
	hasRow, err := stmt.Step()
	if err != nil {
		return false, fmt.Errorf("failed to execute statement: %w", err)
	}
	stmt.Reset()
	err = stmt.ClearBindings()
	if err != nil {
		return false, fmt.Errorf("failed to clear bindings: %w", err)
	}
	return hasRow, nil
}

func GetAllUsers(conn *sqlite.Conn) []User {
	stmt := conn.Prep(
		`SELECT
			u.id,
			u.username,
			u.nickname,
			u.status,
			r.role_id
		FROM
			users u
		LEFT JOIN
			user_roles r ON u.id = r.user_id
		ORDER BY u.id;`,
	)
	defer stmt.Reset()

	users := []User{}

	var currentUser *User = nil
	for hasRow, _ := stmt.Step(); hasRow; hasRow, _ = stmt.Step() {
		user := User{
			ID:       Snowflake(stmt.GetInt64("id")),
			Username: stmt.GetText("username"),
			Nickname: stmt.GetText("nickname"),
			Status:   int(stmt.GetInt64("status")),
			Roles:    []Snowflake{},
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

func GetAllChannels(conn *sqlite.Conn) []Channel {
	stmt := conn.Prep(
		`SELECT
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
			channel_user_permissions cup ON c.id = cup.channel_id
		ORDER BY c.id;`,
	)
	defer stmt.Reset()

	channels := []Channel{}
	var currentChannel *Channel = nil

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

		// Handle role overwrites
		if !stmt.IsNull("role_id") {
			overwrite := Overwrite{
				ID:    Snowflake(stmt.GetInt64("role_id")),
				Type:  OverwriteTypeRole,
				Allow: int(stmt.GetInt64("role_allow")),
				Deny:  int(stmt.GetInt64("role_deny")),
			}
			channel.Overwrites = append(channel.Overwrites, overwrite)
		}

		// Handle user overwrites
		if !stmt.IsNull("user_id") {
			overwrite := Overwrite{
				ID:    Snowflake(stmt.GetInt64("user_id")),
				Type:  OverwriteTypeUser,
				Allow: int(stmt.GetInt64("user_allow")),
				Deny:  int(stmt.GetInt64("user_deny")),
			}
			channel.Overwrites = append(channel.Overwrites, overwrite)
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

func GetAllRoles(conn *sqlite.Conn) []Role {
	stmt := conn.Prep(
		`SELECT
			id,
			name,
			color,
			position,
			allow,
			deny,
			hoisted,
			mentionable
		FROM
			roles
		ORDER BY id;`,
	)
	defer stmt.Reset()

	roles := []Role{}

	for hasRow, _ := stmt.Step(); hasRow; hasRow, _ = stmt.Step() {
		role := Role{
			ID:          Snowflake(stmt.GetInt64("id")),
			Name:        stmt.GetText("name"),
			Color:       int(stmt.GetInt64("color")),
			Position:    int(stmt.GetInt64("position")),
			Allow:       int(stmt.GetInt64("allow")),
			Deny:        int(stmt.GetInt64("deny")),
			Hoisted:     stmt.GetInt64("hoisted") != 0,
			Mentionable: stmt.GetInt64("mentionable") != 0,
		}
		roles = append(roles, role)
	}

	return roles
}

func GetAllEmojis(conn *sqlite.Conn) []Emoji {
	stmt := conn.Prep(
		`SELECT
			name,
			id
		FROM
			emojis
		ORDER BY name;`,
	)
	defer stmt.Reset()

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

func UseToken(conn *sqlite.Conn, token string) error {
	stmt := conn.Prep(`
		UPDATE user_tokens
		SET last_used_at = $last_used_at
		WHERE token = $token;`,
	)
	defer stmt.Reset()

	now := time.Now().UnixMilli()
	stmt.SetText("$token", token)
	stmt.SetInt64("$last_used_at", now)
	if _, err := ExecuteStatement(stmt); err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func Authenticate(conn *sqlite.Conn, token string) (Snowflake, error) {
	stmt := conn.Prep(`
		SELECT
			user_id
		FROM
			user_tokens
		WHERE
			token = $token;`,
	)
	defer stmt.Reset()

	stmt.SetText("$token", token)

	hasRow, err := stmt.Step()
	if err != nil {
		return 0, NewError(ErrorCodeInternalError, err)
	}

	if !hasRow {
		return 0, NewError(ErrorCodeInvalidToken, nil)
	}

	if err := UseToken(conn, token); err != nil {
		return 0, err
	}

	userID := Snowflake(stmt.GetInt64("user_id"))
	return userID, nil
}

func AddToken(conn *sqlite.Conn, userID Snowflake) (string, error) {
	stmt := conn.Prep(`
		INSERT INTO user_tokens(user_id, token, created_at, last_used_at)
		VALUES ($user_id, $token, $created_at, $last_used_at);`,
	)

	token := GetRandom256()
	now := time.Now().UnixMilli()

	stmt.SetInt64("$user_id", int64(userID))
	stmt.SetText("$token", token)
	stmt.SetInt64("$created_at", now)
	stmt.SetInt64("$last_used_at", now)

	if _, err := ExecuteStatement(stmt); err != nil {
		return "", NewError(ErrorCodeInternalError, err)
	}

	return token, nil
}

func Login(conn *sqlite.Conn, username, password string) (Snowflake, string, error) {
	stmt := conn.Prep(`
		SELECT
			id,
			hash,
			salt
		FROM
			users
		WHERE
			username = $username`,
	)
	defer stmt.Reset()

	stmt.SetText("$username", username)

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

	if hash != HashPassword(password, salt) {
		return 0, "", NewError(ErrorCodeInvalidCredentials, fmt.Errorf("invalid password"))
	}

	if token, err := AddToken(conn, userID); err != nil {
		return 0, "", err
	} else {
		return userID, token, nil
	}
}

func IsUsernameValid(conn *sqlite.Conn, username string) (bool, error) {
	if username == "" || len(username) < 3 || len(username) > 32 {
		return false, NewError(ErrorCodeInvalidUsername, nil)
	}

	stmt := conn.Prep(`
		SELECT
			username
		FROM
			users
		WHERE
			username = $username;`,
	)
	defer stmt.Reset()

	stmt.SetText("$username", username)

	ok, _ := ExecuteStatement(stmt)

	if ok {
		return false, NewError(ErrorCodeTakenUsername, nil)
	}

	return true, nil
}

func Register(conn *sqlite.Conn, username, password, email, inviteCode string) (Snowflake, string, error) {
	if _, err := IsUsernameValid(conn, username); err != nil {
		return 0, "", err
	}

	stmt := conn.Prep(`
		INSERT INTO users(id, username, hash, salt, email, invite_code)
		VALUES ($id, $username, $hash, $salt, $email, $invite_code);`,
	)
	defer stmt.Reset()

	userID := snowflake.New()
	salt := GetRandom128()
	hash := HashPassword(password, salt)

	stmt.SetInt64("$id", int64(userID))
	stmt.SetText("$username", username)
	stmt.SetText("$hash", hash)
	stmt.SetText("$salt", salt)

	if email != "" {
		stmt.SetText("$email", email)
	} else {
		stmt.SetNull("$email")
	}

	if inviteCode != "" {
		stmt.SetText("$invite_code", inviteCode)
	} else {
		stmt.SetNull("$invite_code")
	}

	if _, err := ExecuteStatement(stmt); err != nil {
		return 0, "", NewError(ErrorCodeInternalError, err)
	}

	token, err := AddToken(conn, userID)
	if err != nil {
		return 0, "", err
	}

	return userID, token, nil
}

func GetSettings(conn *sqlite.Conn) (Settings, error) {
	stmt := conn.Prep(`
		SELECT
			site_name,
			login_message,
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
	defer stmt.Reset()

	hasRow, err := stmt.Step()
	if err != nil {
		return Settings{}, NewError(ErrorCodeInternalError, err)
	}

	if !hasRow {
		return Settings{}, NewError(ErrorCodeInternalError, fmt.Errorf("settings not found"))
	}

	settings := Settings{
		SiteName:         stmt.GetText("site_name"),
		LoginMessage:     stmt.GetText("login_message"),
		UsesEmail:        stmt.GetInt64("uses_email") != 0,
		UsesInviteCodes:  stmt.GetInt64("uses_invite_code") != 0,
		UsesCaptcha:      stmt.GetInt64("uses_captcha") != 0,
		UsesLoginCaptcha: stmt.GetInt64("uses_login_captcha") != 0,
		CaptchaSiteKey:   stmt.GetText("captcha_site_key"),
		CaptchaSecretKey: stmt.GetText("captcha_secret_key"),
	}

	return settings, nil
}

func SetSettings(conn *sqlite.Conn, settings Settings) error {
	stmt := conn.Prep(`
		UPDATE settings
		SET
			site_name = $site_name,
			login_message = $login_message,
			uses_email = $uses_email,
			uses_invite_code = $uses_invite_code,
			uses_captcha = $uses_captcha,
			uses_login_captcha = $uses_login_captcha,
			captcha_site_key = $captcha_site_key,
			captcha_secret_key = $captcha_secret_key
		WHERE id = 0;`,
	)
	defer stmt.Reset()

	stmt.SetText("$site_name", settings.SiteName)
	stmt.SetText("$login_message", settings.LoginMessage)
	stmt.SetInt64("$uses_email", int64(BoolToInt(settings.UsesEmail)))
	stmt.SetInt64("$uses_invite_code", int64(BoolToInt(settings.UsesInviteCodes)))
	stmt.SetInt64("$uses_captcha", int64(BoolToInt(settings.UsesCaptcha)))
	stmt.SetInt64("$uses_login_captcha", int64(BoolToInt(settings.UsesLoginCaptcha)))
	stmt.SetText("$captcha_site_key", settings.CaptchaSiteKey)
	stmt.SetText("$captcha_secret_key", settings.CaptchaSecretKey)

	_, err := ExecuteStatement(stmt)
	if err != nil {
		return NewError(ErrorCodeInternalError, err)
	}

	return nil
}

func GetUser(conn *sqlite.Conn, id Snowflake) (User, error) {
	stmt := conn.Prep(`
		SELECT
			u.id,
			u.username,
			u.nickname,
			u.status,
			r.role_id
		FROM
			users u
		LEFT JOIN
			user_roles r ON u.id = r.user_id
		WHERE
			u.id = $id;`,
	)
	defer stmt.Reset()

	stmt.SetInt64("$id", int64(id))

	hasRow, err := stmt.Step()
	if err != nil {
		return User{}, NewError(ErrorCodeInternalError, err)
	}

	if !hasRow {
		return User{}, NewError(ErrorCodeInternalError, nil)
	}

	user := User{
		ID:       Snowflake(stmt.GetInt64("id")),
		Username: stmt.GetText("username"),
		Nickname: stmt.GetText("nickname"),
		Status:   int(stmt.GetInt64("status")),
		Roles:    []Snowflake{},
	}

	for hasRow {
		if !stmt.IsNull("role_id") {
			user.Roles = append(user.Roles, Snowflake(stmt.GetInt64("role_id")))
		}

		hasRow, err = stmt.Step()

		if err != nil {
			return User{}, NewError(ErrorCodeInternalError, err)
		}
	}

	return user, nil
}

//go:embed sql/message_query.sql
var message_query string

// TODO: Maybe split into multiple queries.
func GetMessages(conn *sqlite.Conn, channelID Snowflake, anchorID Snowflake, limit int, before bool) ([]Message, error) {
	// Start with the base query from the embedded SQL file
	baseQuery := strings.TrimSuffix(message_query, ";")

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

	// Prepare the final statement
	stmt, err := conn.Prepare(finalQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare message query: %w", err)
	}
	defer stmt.Reset()

	// Bind parameters to the statement
	for i, param := range params {
		stmt.BindInt64(i+1, param)
	}

	messages, err := parseMessagesFromStmt(conn, stmt)
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

func GetMessage(conn *sqlite.Conn, id Snowflake) (Message, error) {
	baseQuery := strings.TrimSuffix(message_query, ";")
	finalQuery := baseQuery + ` WHERE m.id = $id;`

	stmt, err := conn.Prepare(finalQuery)
	if err != nil {
		return Message{}, fmt.Errorf("failed to prepare message query: %w", err)
	}
	defer stmt.Reset()

	stmt.SetInt64("$id", int64(id))

	messages, err := parseMessagesFromStmt(conn, stmt)
	if err != nil {
		return Message{}, err
	}

	if len(messages) == 0 {
		return Message{}, fmt.Errorf("message not found")
	}

	return messages[0], nil
}

func parseMessagesFromStmt(conn *sqlite.Conn, stmt *sqlite.Stmt) ([]Message, error) {

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

func GetFile(conn *sqlite.Conn, name string) (*File, error) {
	stmt := conn.Prep(`
		SELECT
			rowid,
			sz AS size,
			mtime AS modified
		FROM
			sqlar s
		WHERE
			name = $name`,
	)

	stmt.SetText("$name", name)

	hasRow, err := stmt.Step()
	if err != nil {
		panic(err)
	}
	defer stmt.Reset()

	if !hasRow {
		disk, err := os.Open(filepath.Join(DataFolder, name))
		if err == nil {
			stat, err := disk.Stat()
			if err != nil {
				disk.Close()
				return nil, fmt.Errorf("failed to stat file: %w", err)
			}
			return &File{
				Name:     name,
				Size:     int(stat.Size()),
				Modified: stat.ModTime(),
				Content:  &DiskReader{File: disk},
			}, nil
		}
		return nil, ErrFileNotFound
	}

	rowid := stmt.GetInt64("rowid")
	content, err := conn.OpenBlob("main", "sqlar", "data", rowid, false)

	if err != nil {
		return nil, fmt.Errorf("failed to open blob: %w", err)
	}

	return &File{
		Name:     name,
		Size:     int(stmt.GetInt64("size")),
		Modified: time.Unix(stmt.GetInt64("modified"), 0),
		Content:  content,
	}, nil
}

func GetPreview(conn *sqlite.Conn, id Snowflake, typ string) (*File, error) {
	name := GetPreviewFilename(id, typ)
	content, err := GetFile(conn, name)

	if err != nil {
		return nil, err
	}

	content.Mimetype = "image/webp"
	return content, nil
}

func GetAttachment(conn *sqlite.Conn, id Snowflake, filename string) (*File, error) {
	//fmt.Println("GetAttachment", id, filename)

	stmt := conn.Prep(`
		SELECT 
			filename,
			mimetype,
			path
		FROM 
				attachments a
		WHERE 
				id = $id AND filename = $filename;`,
	)
	stmt.SetInt64("$id", int64(id))
	stmt.SetText("$filename", filename)

	hasRow, err := stmt.Step()
	if err != nil {
		panic(err)
	}

	if hasRow {
		name := stmt.GetText("filename")
		mimetype := stmt.GetText("mimetype")
		path := stmt.GetText("path")
		stmt.Reset()
		file, err := GetFile(conn, path)
		if err != nil {
			fmt.Println(path)
			return nil, fmt.Errorf("failed to get file: %w", err)
		}

		file.Mimetype = mimetype
		file.Name = name

		return file, nil
	} else {
		stmt.Reset()
		return nil, ErrFileNotFound
	}

}

func GetAttachmentFilename(id Snowflake, filename string) string {
	return fmt.Sprintf("attachments/%d/%s", id, filename)
}

func GetPreviewFilename(id Snowflake, typ string) string {
	return fmt.Sprintf("previews/%d/%s.webp", id, typ)
}

func AddFile(conn *sqlite.Conn, name string, content FileInputReader) error {
	size := content.Size()

	if size > MaxDatabaseFileSize {
		file := filepath.Join(DataFolder, name)
		os.MkdirAll(filepath.Dir(file), 0755)

		disk, err := os.Create(file)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
		defer disk.Close()

		_, err = io.Copy(disk, content)
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}

		return nil
	}

	stmt := conn.Prep(`
		INSERT INTO sqlar(name, mode, mtime, sz, data)
		VALUES ($name, 420, strftime('%s','now'), $size, zeroblob($size));`,
	)
	defer stmt.Reset()

	stmt.SetText("$name", name)
	stmt.SetInt64("$size", size)

	_, err := stmt.Step()
	if err != nil {
		panic(err)
	}

	blob, err := conn.OpenBlob("main", "sqlar", "data", conn.LastInsertRowID(), true)
	if err != nil {
		return fmt.Errorf("failed to open blob: %w", err)
	}

	_, write_err := io.Copy(blob, content)
	blob_err := blob.Close()
	content_err := content.Close()
	if write_err != nil {
		return fmt.Errorf("failed to write blob: %w", write_err)
	}
	if blob_err != nil {
		return fmt.Errorf("failed to close blob: %w", blob_err)
	}
	if content_err != nil {
		return fmt.Errorf("failed to close content: %w", content_err)
	}

	return nil
}

func AddPreviews(conn *sqlite.Conn, id Snowflake, previews *Previews) error {
	sqlar_stmt := conn.Prep(`
		INSERT INTO sqlar(name, mode, mtime, sz, data) 
		VALUES
			($display, 420, strftime('%s','now'), length($display_data), $display_data),
			($preview, 420, strftime('%s','now'), length($preview_data), $preview_data),
			($blur, 420, strftime('%s','now'), length($blur_data), $blur_data);`,
	)
	defer sqlar_stmt.Reset()

	preview_stmt := conn.Prep(`
		INSERT INTO previews(id, width, height, display, preview, blur)
		VALUES ($id, $width, $height, $display, $preview, $blur);`,
	)
	defer preview_stmt.Reset()

	display_path := GetPreviewFilename(id, "display")
	preview_path := GetPreviewFilename(id, "preview")
	blur_path := GetPreviewFilename(id, "blur")

	sqlar_stmt.SetText("$display", display_path)
	sqlar_stmt.SetText("$preview", preview_path)
	sqlar_stmt.SetText("$blur", blur_path)
	sqlar_stmt.SetBytes("$display_data", previews.Display)
	sqlar_stmt.SetBytes("$preview_data", previews.Preview)
	sqlar_stmt.SetBytes("$blur_data", previews.Blur)
	_, err := sqlar_stmt.Step()
	if err != nil {
		return err
	}

	preview_stmt.SetInt64("$id", int64(id))
	preview_stmt.SetInt64("$width", int64(previews.Width))
	preview_stmt.SetInt64("$height", int64(previews.Height))
	preview_stmt.SetText("$display", display_path)
	preview_stmt.SetText("$preview", preview_path)
	preview_stmt.SetText("$blur", blur_path)
	_, err = preview_stmt.Step()
	if err != nil {
		return err
	}

	return nil
}

func AddAttachment(conn *sqlite.Conn, messageID Snowflake, attachment *Attachment) error {
	attach_stmt := conn.Prep(`
		INSERT INTO attachments(id, message_id, type, mimetype, filename, path)
		VALUES ($id, $message_id, $type, $mimetype, $filename, $path);`,
	)

	attach_stmt.SetInt64("$id", int64(attachment.ID))
	attach_stmt.SetInt64("$message_id", int64(messageID))
	attach_stmt.SetInt64("$type", int64(attachment.Type))
	attach_stmt.SetText("$mimetype", attachment.MimeType)
	attach_stmt.SetText("$filename", attachment.Filename)
	attach_stmt.SetText("$path", GetAttachmentFilename(attachment.ID, attachment.Filename))

	if _, err := ExecuteStatement(attach_stmt); err != nil {
		return fmt.Errorf("failed to insert attachment: %w", err)
	}

	return nil
}

func AddEmbed(conn *sqlite.Conn, messageID Snowflake, embed *Embed) error {
	// Assign a new ID to the embed if not already set
	if embed.ID == 0 {
		embed.ID = snowflake.New()
	}

	// Prepare the INSERT statement for the embeds table
	embed_stmt := conn.Prep(`
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
	defer embed_stmt.Reset()

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
	_, err := embed_stmt.Step()
	if err != nil {
		return fmt.Errorf("failed to insert embed: %w", err)
	}

	// Insert embed fields into the embed_fields table
	for _, field := range embed.Fields {
		field_stmt := conn.Prep(`
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
		defer field_stmt.Reset()

		field_stmt.SetInt64("$embed_id", int64(embed.ID))
		field_stmt.SetText("$name", field.Name)
		field_stmt.SetText("$value", field.Value)
		if field.Inline {
			field_stmt.SetInt64("$inline", 1)
		} else {
			field_stmt.SetInt64("$inline", 0)
		}

		_, err := field_stmt.Step()
		if err != nil {
			return fmt.Errorf("failed to insert embed field '%s': %w", field.Name, err)
		}
	}
	return nil
}

func AddMessage(conn *sqlite.Conn, message *Message) {
	messages_stmt := conn.Prep("INSERT INTO messages (id, type, channel_id, timestamp, author_id, content) VALUES ($id, $type, $channel_id, $timestamp, $author_id, $content);")
	message_user_mentions_stmt := conn.Prep("INSERT INTO message_user_mentions (message_id, user_id) VALUES ($message_id, $user_id);")
	message_role_mentions_stmt := conn.Prep("INSERT INTO message_role_mentions (message_id, role_id) VALUES ($message_id, $role_id);")
	message_channel_mentions_stmt := conn.Prep("INSERT INTO message_channel_mentions (message_id, channel_id) VALUES ($message_id, $channel_id);")

	commit := sqlitex.Transaction(conn)
	defer func() {
		if r := recover(); r != nil {
			var err error = r.(error)
			commit(&err)
			panic(r)
		}
	}()

	messages_stmt.SetInt64("$id", int64(message.ID))
	messages_stmt.SetInt64("$type", int64(message.Type))
	messages_stmt.SetInt64("$channel_id", int64(message.ChannelID))
	messages_stmt.SetInt64("$timestamp", int64(message.Timestamp))
	messages_stmt.SetInt64("$author_id", int64(message.AuthorID))
	messages_stmt.SetText("$content", message.Content)

	if _, err := ExecuteStatement(messages_stmt); err != nil {
		commit(&err)
		panic(err)
	}

	for _, mentionedUserID := range message.MentionedUsers {
		message_user_mentions_stmt.SetInt64("$message_id", int64(message.ID))
		message_user_mentions_stmt.SetInt64("$user_id", int64(mentionedUserID))
		if _, err := ExecuteStatement(message_user_mentions_stmt); err != nil {
			commit(&err)
			panic(err)
		}
	}

	for _, mentionedRoleID := range message.MentionedRoles {
		message_role_mentions_stmt.SetInt64("$message_id", int64(message.ID))
		message_role_mentions_stmt.SetInt64("$role_id", int64(mentionedRoleID))
		ExecuteStatement(message_role_mentions_stmt)
	}

	for _, mentionedChannelID := range message.MentionedChannels {
		message_channel_mentions_stmt.SetInt64("$message_id", int64(message.ID))
		message_channel_mentions_stmt.SetInt64("$channel_id", int64(mentionedChannelID))
		ExecuteStatement(message_channel_mentions_stmt)
	}

	for _, embed := range message.Embeds {
		if err := AddEmbed(conn, message.ID, &embed); err != nil {
			commit(&err)
			panic(err)
		}
	}

	for _, attachment := range message.Attachments {
		if err := AddAttachment(conn, message.ID, &attachment); err != nil {
			commit(&err)
			panic(err)
		}
	}

	// Commit the transaction
	var noError error = nil
	commit(&noError)
}

func UploadAttachment(conn *sqlite.Conn, messageID Snowflake, filename string, input FileInputReader) {
	commit := sqlitex.Transaction(conn)
	path := GetAttachmentFilename(messageID, filename)
	err := AddFile(conn, path, input)
	if err != nil {
		commit(&err)
		panic(err)
	}

	file, err := GetFile(conn, path)
	if err != nil {
		commit(&err)
		panic(err)
	}
	defer file.Content.Close()

	attach_stmt := conn.Prep(`
		INSERT INTO attachments(id, message_id, type, mimetype, filename, path)
		VALUES ($id, $message_id, $type, $mimetype, $filename, $path);`,
	)

	id := snowflake.New()
	typ := AttachmentTypeFile

	file.Content.Seek(0, io.SeekStart)
	mime, _ := mimetype.DetectReader(file.Content)

	mimeType := mime.String()

	if slices.Contains(SupportedImageTypes, mimeType) {
		typ = AttachmentTypeImage
	}
	if slices.Contains(SupportedVideoTypes, mimeType) {
		typ = AttachmentTypeVideo
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	attach_stmt.SetInt64("$id", int64(id))
	attach_stmt.SetInt64("$message_id", int64(messageID))
	attach_stmt.SetInt64("$type", int64(typ))
	attach_stmt.SetText("$mimetype", mimeType)
	attach_stmt.SetText("$filename", filename)
	attach_stmt.SetText("$path", path)
	_, err = attach_stmt.Step()
	if err != nil {
		commit(&err)
		panic(err)
	}
	var noError error = nil
	commit(&noError)

	if typ != AttachmentTypeFile {
		file.Content.Seek(0, io.SeekStart)
		previews, err := GetPreviews(file.Content, false)
		if err != nil {
			fmt.Println("Failed to generate previews:", err)
			typ = AttachmentTypeFile
		} else {
			err = AddPreviews(conn, id, previews)
			if err != nil {
				panic(err)
			}
		}
	}
}

func BuildAttachmentFromFile(conn *sqlite.Conn, id Snowflake, path string) (*Attachment, error) {
	file, err := GetFile(conn, path)
	if err != nil {
		return nil, err
	}
	defer file.Content.Close()

	file.Content.Seek(0, io.SeekStart)
	mime, _ := mimetype.DetectReader(file.Content)

	mimeType := mime.String()

	typ := AttachmentTypeFile

	if slices.Contains(SupportedImageTypes, mimeType) {
		typ = AttachmentTypeImage
	}
	if slices.Contains(SupportedVideoTypes, mimeType) {
		typ = AttachmentTypeVideo
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	width, height := 0, 0

	if typ != AttachmentTypeFile {
		file.Content.Seek(0, io.SeekStart)
		previews, err := GetPreviews(file.Content, false)
		if err != nil {
			fmt.Println("Failed to generate previews:", err)
			typ = AttachmentTypeFile
		} else {
			width = previews.Width
			height = previews.Height

			err = AddPreviews(conn, id, previews)
			if err != nil {
				panic(err)
			}
		}
	}

	attachment := &Attachment{
		ID:       id,
		Filename: path,
		Type:     typ,
		MimeType: mimeType,
		Size:     int(file.Size),
		Width:    width,
		Height:   height,
	}

	return attachment, nil
}

func CheckURLIsAllowed(conn *sqlite.Conn, embedID Snowflake, requestedURL string) (bool, error) {
	stmt := conn.Prep(`
		SELECT
			external_url
		FROM
			external_urls
		WHERE
			id = $embed_id;`)
	defer stmt.Reset()

	stmt.SetInt64("$embed_id", int64(embedID))

	hasRow, err := stmt.Step()
	if err != nil {
		return false, fmt.Errorf("failed to execute query: %w", err)
	}

	if !hasRow {
		return false, ErrFileNotFound // Or a more specific error like ErrEmbedNotFound
	}

	videoURL := stmt.GetText("external_url")

	return videoURL == requestedURL, nil
}
