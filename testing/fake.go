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
	"time"

	"github.com/icrowley/fake"
	"zombiezen.com/go/sqlite"
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

	db, err := storage.OpenConnection(ctx)
	if err != nil {
		panic(err)
	}
	defer storage.CloseConnection(db)

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

	tx := storage.NewTransaction(db)
	tx.Start()
	err = tx.SetSettings(settings)
	tx.Commit(err)

	rng := rand.New(rand.NewSource(101))

	roles := [3]Snowflake{0, 0, 0}

	tx.Start()
	roles[0], err = tx.AddRole("Admin", 0xa84300, 0, PermissionAdministrator, true, true)
	if err != nil {
		panic(err)
	}
	roles[1], err = tx.AddRole("Moderator", 0xe67e22, 1, PermissionManageChannels|PermissionManageRoles|PermissionManageMessages, true, true)
	if err != nil {
		panic(err)
	}
	roles[2], err = tx.AddRole("Member", 0xf1c40f, 2, PermissionDefault, false, false)
	if err != nil {
		panic(err)
	}
	tx.Commit(nil)

	const userCount = 1000
	users := [userCount]Snowflake{}

	tx.Start()
	for i := 0; i < userCount; i++ {
		var password = fake.SimplePassword()
		var userName = fake.UserName() + GetRandom128()[:5]

		for len(userName) < 3 || len(userName) > 32 {
			userName = fake.UserName() + GetRandom128()[:5]
		}

		var displayName = fake.FullName()
		var presence = rng.Int63n(3)
		var salt = GetRandom128()
		var hash = HashSha256(HashSha256(password, ""), salt)
		var status = ""
		if rng.Intn(5) == 0 {
			status = fake.Sentence()
		}

		var description = ""
		if rng.Intn(2) == 0 {
			description = fake.Paragraph()
			if len(description) > 250 {
				description = description[:250]
			}
		}

		users[i], err = tx.AddUser(userName, hash, salt, "", "")
		if err != nil {
			fmt.Printf("Failed to add user %d: %v\n", i, err.Error())
			panic(err)
		}

		err = tx.SetUserProfile(users[i], displayName, status, description, ProfileColorDefault, AvatarModifiedDefault)
		if err != nil {
			fmt.Printf("Failed to set profile messages for user %d: %v\n", i, err.Error())
			panic(err)
		}

		err = tx.SetUserPresence(users[i], int(presence))
		if err != nil {
			fmt.Printf("Failed to set presence for user %d: %v\n", i, err.Error())
			panic(err)
		}

		if i == 0 {
			err = tx.AddRoleToUser(users[i], roles[0])
			if err != nil {
				panic(err)
			}
		} else if i <= 2 {
			err = tx.AddRoleToUser(users[i], roles[1])
			if err != nil {
				panic(err)
			}
		} else {
			err = tx.AddRoleToUser(users[i], roles[2])
			if err != nil {
				panic(err)
			}
		}

		if i%1000 == 0 {
			tx.Commit(nil)
			tx.Start()
		}
	}

	tx.Commit(nil)

	createMainUser(db, roles[2])

	const channelCount = 8
	channels := [channelCount]Snowflake{}

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

	tx.Start()
	generalID, err := tx.AddChannel("General", ChannelTypeCategory, "", 0, 0)
	if err != nil {
		panic(err)
	}

	for i := 0; i < channelCount; i++ {
		channels[i], err = tx.AddChannel(fake.Company(), ChannelTypeText, "", i, generalID)
		if err != nil {
			panic(err)
		}
	}
	tx.Commit(nil)

	for i := 0; i < channelCount; i++ {
		for k := 0; k < 200; k++ {
			url := ""
			if 199-k < len(URLs) && i == 0 {
				url = URLs[199-k]
			}

			id := createMessage(db, channels[i], users[:], roles[:], channels[:], url, rng)

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
	var password = "password"
	var salt = GetRandom128()
	var hash = HashSha256(HashSha256(password, ""), salt)

	tx := storage.NewTransaction(db)
	tx.Start()

	id, _ := tx.AddUser("user", hash, salt, "", "")
	_ = tx.SetUserProfile(id, "User", "", "", ProfileColorDefault, AvatarModifiedDefault)
	_ = tx.SetUserPresence(id, UserPresenceOnline)

	tx.AddRoleToUser(id, role)

	tx.Commit(nil)
}

func createMessage(db *sqlite.Conn, channelID Snowflake, users []Snowflake, roles []Snowflake, channels []Snowflake, url string, rng *rand.Rand) Snowflake {
	/*message := db.Prep("INSERT INTO messages (id, type, channel_id, timestamp, author_id, content) VALUES ($id, $type, $channel_id, $timestamp, $author_id, $content);")
	messageUserMention := db.Prep("INSERT INTO message_user_mentions (message_id, user_id) VALUES ($message_id, $user_id);")
	messageRoleMention := db.Prep("INSERT INTO message_role_mentions (message_id, role_id) VALUES ($message_id, $role_id);")
	messageChannelMention := db.Prep("INSERT INTO message_channel_mentions (message_id, channel_id) VALUES ($message_id, $channel_id);")
	*/
	//messageID := snowflake.New()
	userID := users[rng.Intn(len(users))] // random user
	content := fake.Paragraph()           //fake.Sentence()

	if url != "" {
		content += "\n\n" + url
	}

	var msg Message
	msg.ID = snowflake.New()
	msg.AuthorID = userID
	msg.ChannelID = channelID
	msg.Content = content
	msg.Type = MessageTypeDefault
	msg.Timestamp = int(time.Now().UnixMilli())

	mentionedUsers, mentionedRoles, mentionedChannels, embeddableURLs := chat.ParseMessageContent(content)
	msg.MentionedUsers = mentionedUsers
	msg.MentionedRoles = mentionedRoles
	msg.MentionedChannels = mentionedChannels
	msg.EmbeddableURLs = embeddableURLs

	tx := storage.NewTransaction(db)
	tx.Start()
	err := tx.AddMessage(&msg)
	if err != nil {
		panic(err)
	}
	tx.Commit(nil)

	for _, embeddableURL := range embeddableURLs {
		embed, err := chat.GetEmbedFromURL(context.Background(), msg.ID, embeddableURL)
		if err != nil {
			fmt.Println(err)
			continue
		}

		tx := storage.NewTransaction(db)
		tx.Start()
		err = tx.AddEmbed(msg.ID, embed)
		if err != nil {
			panic(err)
		}
		tx.Commit(err)
	}

	return msg.ID
}

func createAttachment(db *sqlite.Conn, messageID Snowflake, path string) {
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	reader := DiskReader{File: file}

	name := filepath.Base(path)

	attachmentID := snowflake.New()

	attachment, err := storage.UploadAttachment(messageID, attachmentID, name, &reader)
	if err != nil {
		panic(err)
	}

	tx := storage.NewTransaction(db)
	tx.Start()
	err = tx.AddAttachment(messageID, attachment)
	if err != nil {
		panic(err)
	}
	tx.Commit(err)
}
