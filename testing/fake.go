package testing

import (
	"clack/chat"
	. "clack/common"
	"clack/common/snowflake"
	"clack/storage"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/icrowley/fake"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func executeStatement(stmt *sqlite.Stmt) {
	if _, err := stmt.Step(); err != nil {
		panic(err)
	}
	stmt.Reset()
	err := stmt.ClearBindings()
	if err != nil {
		panic(err)
	}
}

func tryExecuteStatement(stmt *sqlite.Stmt) bool {
	_, err := stmt.Step()
	stmt.Reset()
	stmt.ClearBindings()
	return err == nil
}

func PopulateDatabase(ctx context.Context) {
	// populate database with fake data

	db, err := storage.OpenDatabase(ctx)
	if err != nil {
		panic(err)
	}
	defer storage.CloseDatabase(db)

	var settings Settings = Settings{
		SiteName:           "Clack",
		LoginMessage:       "Welcome back!",
		DefaultPermissions: PermissionDefault,
		UsesEmail:          false,
		UsesInviteCodes:    false,
		UsesCaptcha:        true,
		UsesLoginCaptcha:   false,
		CaptchaSiteKey:     "052869f2-0556-4700-9b48-8bf6807003b6",
		CaptchaSecretKey:   "ES_040e28547c354609b8d8c0e7a630f6d3",
	}

	storage.SetSettings(db, settings)

	rng := rand.New(rand.NewSource(101))

	roles := [3]Snowflake{
		snowflake.New(),
		snowflake.New(),
		snowflake.New(),
	}
	role := db.Prep("INSERT INTO roles (id, name, color, position, permissions, hoisted, mentionable) VALUES ($id, $name, $color, $position, $permissions, $hoisted, $mentionable);")

	role.SetInt64("$id", int64(roles[0]))
	role.SetText("$name", "Admin")
	role.SetInt64("$color", 0xa84300)
	role.SetInt64("$position", 0)
	role.SetBool("$hoisted", true)
	role.SetBool("$mentionable", true)
	role.SetInt64("$permissions", PermissionAdministrator)
	executeStatement(role)

	role.SetInt64("$id", int64(roles[1]))
	role.SetText("$name", "Moderator")
	role.SetInt64("$color", 0xe67e22)
	role.SetInt64("$position", 1)
	role.SetBool("$hoisted", true)
	role.SetBool("$mentionable", true)
	role.SetInt64("$permissions", PermissionManageChannels|PermissionManageRoles|PermissionManageMessages)
	executeStatement(role)

	role.SetInt64("$id", int64(roles[2]))
	role.SetText("$name", "Member")
	role.SetInt64("$color", 0xf1c40f)
	role.SetInt64("$position", 2)
	executeStatement(role)

	commit := sqlitex.Transaction(db)

	user := db.Prep("INSERT INTO users (id, username, nickname, status, hash, salt) VALUES ($id, $username, $nickname, $status, $hash, $salt);")
	user_role := db.Prep("INSERT INTO user_roles (user_id, role_id) VALUES ($user_id, $role_id);")

	const userCount = 1000
	users := [userCount]Snowflake{}

	for i := 0; i < userCount; i++ {
		userID := snowflake.New()
		users[i] = userID

		var password = fake.SimplePassword()
		var salt = GetRandom128()
		var hash = HashPassword(HashPassword(password, ""), salt)

		user.SetInt64("$id", int64(userID))
		user.SetText("$username", fake.UserName()+GetRandom128()[:5])
		user.SetText("$nickname", fake.FullName())
		user.SetText("$hash", hash)
		user.SetText("$salt", salt)
		user.SetInt64("$status", rng.Int63n(3))
		executeStatement(user)

		if i == 0 {
			user_role.SetInt64("$user_id", int64(userID))
			user_role.SetInt64("$role_id", int64(roles[0]))
			executeStatement(user_role)
		} else if i <= 2 {
			user_role.SetInt64("$user_id", int64(userID))
			user_role.SetInt64("$role_id", int64(roles[1]))
			executeStatement(user_role)
		} else {
			user_role.SetInt64("$user_id", int64(userID))
			user_role.SetInt64("$role_id", int64(roles[2]))
			executeStatement(user_role)
		}

		if i%1000 == 0 {
			var noError error = nil
			commit(&noError)
			commit = sqlitex.Transaction(db)
		}
	}

	var noError error = nil
	commit(&noError)

	createMainUser(db, roles[2])

	channel := db.Prep("INSERT INTO channels (id, type, name, description, position, parent_id) VALUES ($id, $type, $name, $description, $position, $parent_id);")

	const channelCount = 8
	channels := [channelCount]Snowflake{}

	generalID := snowflake.New()
	channel.SetInt64("$id", int64(generalID))
	channel.SetInt64("$type", ChannelTypeCategory)
	channel.SetText("$name", "General")
	channel.SetText("$description", "")
	channel.SetInt64("$position", 0)
	channel.SetNull("$parent_id")
	executeStatement(channel)

	// Get all file in attachmentDir
	attachmentDir := "../media/test"
	entries, _ := os.ReadDir(attachmentDir)
	attachments := [][]string{}
	for _, entry := range entries {
		path := filepath.Join(attachmentDir, entry.Name())
		if !entry.IsDir() {
			attachments = append(attachments, []string{path})
		} else {
			subEntries, _ := os.ReadDir(path)
			subAttachments := []string{}
			for _, subEntry := range subEntries {
				subPath := filepath.Join(path, subEntry.Name())
				if !subEntry.IsDir() {
					subAttachments = append(subAttachments, subPath)
				}
			}
			attachments = append(attachments, subAttachments)
		}
	}

	URLs := []string{
		"Wow",
		"https://fxtwitter.com/JJ_Animation/status/1411179267342360576",
	}

	c := 0
	for i := 0; i < channelCount; i++ {
		channelID := snowflake.New()
		channels[i] = channelID
		channel.SetInt64("$id", int64(channelID))
		channel.SetInt64("$type", ChannelTypeText)
		channel.SetText("$name", fake.Company())
		channel.SetText("$description", "")
		channel.SetInt64("$position", int64(i))
		channel.SetInt64("$parent_id", int64(generalID))
		executeStatement(channel)

		for k := 0; k < 200; k++ {

			url := ""
			if 199-k < len(URLs) && i == 0 {
				url = URLs[199-k]
			}

			id := createMessage(db, channelID, users[:], roles[:], channels[:], url, rng)
			c += 1

			if 199-k < len(attachments) && i == 0 {
				idx := 199 - k
				for _, path := range attachments[idx] {
					createAttachment(db, id, path)
				}
			}
		}
	}
}

func createMainUser(db *sqlite.Conn, role Snowflake) {
	user := db.Prep("INSERT INTO users (id, username, nickname, status, hash, salt) VALUES ($id, $username, $nickname, $status, $hash, $salt);")

	id := snowflake.New()
	var password = "password"
	var salt = GetRandom128()
	var hash = HashPassword(HashPassword(password, ""), salt)

	user.SetInt64("$id", int64(id))
	user.SetText("$username", "user")
	user.SetText("$nickname", "User")
	user.SetText("$hash", hash)
	user.SetText("$salt", salt)
	user.SetInt64("$status", UserStatusOnline)
	executeStatement(user)

	user_role := db.Prep("INSERT INTO user_roles (user_id, role_id) VALUES ($user_id, $role_id);")
	user_role.SetInt64("$user_id", int64(id))
	user_role.SetInt64("$role_id", int64(role))
	executeStatement(user_role)
}

func createMessage(db *sqlite.Conn, channelID Snowflake, users []Snowflake, roles []Snowflake, channels []Snowflake, url string, rng *rand.Rand) Snowflake {
	message := db.Prep("INSERT INTO messages (id, type, channel_id, timestamp, author_id, content) VALUES ($id, $type, $channel_id, $timestamp, $author_id, $content);")
	messageUserMention := db.Prep("INSERT INTO message_user_mentions (message_id, user_id) VALUES ($message_id, $user_id);")
	messageRoleMention := db.Prep("INSERT INTO message_role_mentions (message_id, role_id) VALUES ($message_id, $role_id);")
	messageChannelMention := db.Prep("INSERT INTO message_channel_mentions (message_id, channel_id) VALUES ($message_id, $channel_id);")

	messageID := snowflake.New()
	userID := users[rng.Intn(len(users))] // random user
	content := fake.Paragraph()           //fake.Sentence()

	numExtras := 0 //1 + rng.Intn(4)
	for i := 0; i < numExtras; i++ {
		extraType := rng.Intn(5)
		switch extraType {
		case 0: // User mention
			if len(users) > 0 {
				mentionedUserID := users[rng.Intn(len(users))]
				content += "\n\n<@" + strconv.FormatInt(int64(mentionedUserID), 10) + "> " + fake.Sentence()
			}
		case 1: // Role mention
			if len(roles) > 0 {
				mentionedRoleID := roles[rng.Intn(len(roles))]
				content += "\n\n<@&" + strconv.FormatInt(int64(mentionedRoleID), 10) + "> " + fake.Sentence()
			}
		case 2: // Channel mention
			if len(channels) > 0 {
				mentionedChannelID := channels[rng.Intn(len(channels))]
				content += "\n\n<#" + strconv.FormatInt(int64(mentionedChannelID), 10) + "> " + fake.Sentence()
			}
		case 3: // Code block
			content += "\n\n```" + fake.Sentence() + "```"
		case 4: // Code inline
			content += "\n\n`" + fake.Sentence() + "`"
		}
	}
	if url != "" {
		content += "\n\n" + url
	}

	message.SetInt64("$id", int64(messageID))
	message.SetInt64("$type", MessageTypeDefault)
	message.SetInt64("$channel_id", int64(channelID))
	message.SetInt64("$timestamp", time.Now().UnixMilli())
	message.SetInt64("$author_id", int64(userID))
	message.SetText("$content", content)
	executeStatement(message)

	mentionedUsers, mentionedRoles, mentionedChannels, embeddableURLs := chat.ParseMessageContent(content)

	for _, mentionedUserID := range mentionedUsers {
		messageUserMention.SetInt64("$message_id", int64(messageID))
		messageUserMention.SetInt64("$user_id", int64(mentionedUserID))
		tryExecuteStatement(messageUserMention)
	}

	for _, mentionedRoleID := range mentionedRoles {
		messageRoleMention.SetInt64("$message_id", int64(messageID))
		messageRoleMention.SetInt64("$role_id", int64(mentionedRoleID))
		tryExecuteStatement(messageRoleMention)
	}

	for _, mentionedChannelID := range mentionedChannels {
		messageChannelMention.SetInt64("$message_id", int64(messageID))
		messageChannelMention.SetInt64("$channel_id", int64(mentionedChannelID))
		tryExecuteStatement(messageChannelMention)
	}

	for _, embeddableURL := range embeddableURLs {
		embed, err := chat.GetEmbedFromURL(context.Background(), db, embeddableURL)
		if err != nil {
			fmt.Println(err)
			continue
		}

		storage.AddEmbed(db, messageID, embed)
	}

	return messageID
}

func createAttachment(db *sqlite.Conn, messageID Snowflake, path string) {
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	reader := DiskReader{File: file}

	name := filepath.Base(path)

	storage.UploadAttachment(db, messageID, name, &reader)
}
