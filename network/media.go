package network

import (
	"clack/common/cache"
	"clack/common/snowflake"
	"clack/storage"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func attachmentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	messageIDInt64, err := strconv.ParseInt(vars["message_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	messageID := snowflake.Snowflake(messageIDInt64)

	attachmentIDInt64, err := strconv.ParseInt(vars["attachment_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid attachment id", http.StatusBadRequest)
		return
	}
	attachmentID := snowflake.Snowflake(attachmentIDInt64)

	attachmentName := vars["attachment_name"]

	conn, err := storage.OpenConnection(r.Context())
	tx := storage.NewTransaction(conn)
	attachment, err := tx.GetAttachment(messageID, attachmentID, attachmentName)
	storage.CloseConnection(conn)

	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			http.Error(w, "attachment not found", http.StatusNotFound)
			return
		}
		srvLog.Printf("Failed to get attachment (Message ID: %d, Attachment ID: %d, Name: %s): %v", messageIDInt64, attachmentIDInt64, attachmentName, err)
		http.Error(w, "failed to get attachment", http.StatusInternalServerError)
		return
	}

	attch, err := storage.GetAttachment(messageID, attachment)

	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			http.Error(w, "attachment not found", http.StatusNotFound)
		} else {
			srvLog.Printf("Failed to get attachment (ID: %d, Name: %s): %v", attachmentID, attachmentName, err)
			http.Error(w, "failed to get attachment", http.StatusInternalServerError)
		}
		return
	}
	//attch.Modified = time.Now()

	w.Header().Set("Content-Type", attch.Mimetype)
	w.Header().Set("Content-Disposition", "inline; filename="+attch.Name)

	http.ServeContent(w, r, attch.Name, attch.Modified, attch.Content)

	attch.Content.Close()
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	messageIDInt64, err := strconv.ParseInt(vars["message_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	messageID := snowflake.Snowflake(messageIDInt64)

	previewIDInt64, err := strconv.ParseInt(vars["preview_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid preview id", http.StatusBadRequest)
		return
	}
	previewID := snowflake.Snowflake(previewIDInt64)

	previewType := r.URL.Query().Get("type")
	if previewType != "thumbnail" && previewType != "display" {
		http.Error(w, "invalid type", http.StatusBadRequest)
		return
	}

	preview, err := storage.GetPreview(messageID, previewID, previewType)
	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			http.Error(w, "preview not found", http.StatusNotFound)
			return
		}
		srvLog.Printf("Failed to get preview (Message ID: %d, Preview ID: %d, Type: %s): %v", messageID, previewID, previewType, err)
		http.Error(w, "failed to get preview", http.StatusInternalServerError)
		return
	}

	//preview.Modified = time.Now()

	w.Header().Set("Content-Type", preview.Mimetype)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")

	http.ServeContent(w, r, preview.Name, preview.Modified, preview.Content)

	preview.Content.Close()
}

func avatarHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	userIDInt64, err := strconv.ParseInt(vars["user_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	userID := snowflake.Snowflake(userIDInt64)

	modified, err := strconv.ParseInt(vars["modified"], 10, 64)
	if err != nil {
		http.Error(w, "invalid modified timestamp", http.StatusBadRequest)
		return
	}

	avatarType := r.URL.Query().Get("type")
	if avatarType != "thumbnail" && avatarType != "display" {
		http.Error(w, "invalid type", http.StatusBadRequest)
		return
	}

	avatar, err := storage.GetAvatar(userID, modified, avatarType)
	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			http.Error(w, "avatar not found", http.StatusNotFound)
			return
		}
		srvLog.Printf("Failed to get avatar (User ID: %d, Modified: %d, Type: %s): %v", userID, modified, avatarType, err)
		http.Error(w, "failed to get avatar", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", avatar.Mimetype)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")

	http.ServeContent(w, r, avatar.Name, avatar.Modified, avatar.Content)
}

func externalHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	messageIDInt64, err := strconv.ParseInt(vars["message_id"], 10, 64)
	if err != nil {
		srvLog.Printf("Invalid message id: %v", err)
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	messageID := snowflake.Snowflake(messageIDInt64)

	embedIDInt64, err := strconv.ParseInt(vars["embed_id"], 10, 64)
	if err != nil {
		srvLog.Printf("Invalid embed id: %v", err)
		http.Error(w, "invalid embed id", http.StatusBadRequest)
		return
	}
	embedID := snowflake.Snowflake(embedIDInt64)

	urlEncoded := r.URL.Query().Get("url")
	if urlEncoded == "" {
		srvLog.Printf("Missing url parameter")
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	url := urlEncoded /*, err := url.QueryUnescape(targetURLEncoded)
	if err != nil {
		srvLog.Printf("Failed to unescape URL: %v", err)
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}*/

	db, err := storage.OpenConnection(context.Background())
	defer storage.CloseConnection(db)

	tx := storage.NewTransaction(db)
	allowed, err := tx.IsURLAllowed(embedID, url)
	if err != nil {
		srvLog.Printf("Failed to check URL: %v", err)
		http.Error(w, "failed to check URL", http.StatusInternalServerError)
		return
	}

	if !allowed {
		fmt.Println("URL not allowed:", urlEncoded, url)
		http.Error(w, "url not allowed", http.StatusForbidden)
		return
	}

	/*db, err := storage.OpenDatabase(context.Background())
	defer storage.CloseDatabase(db)
	if err != nil {
		srvLog.Printf("Failed to open database: %v", err)
		http.Error(w, "failed to open database", http.StatusInternalServerError)
		return
	}

	found, err := storage.CheckURL(db, snowflake.Snowflake(targetEmbedID), targetURLStr)
	if err != nil {
		srvLog.Printf("Failed to check URL: %v", err)
		http.Error(w, "failed to check URL", http.StatusInternalServerError)
		return
	}

	if !found {
		http.Error(w, "url not found", http.StatusNotFound)
		return
	}*/

	w.Header().Set("Accept-Ranges", "bytes")

	err = cache.ServeExternal(w, r, messageID, embedID, url)
	if err != nil {
		if strings.HasPrefix(err.Error(), "origin") {
			http.Error(w, err.Error(), http.StatusBadGateway)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func buildMediaRouter(router *mux.Router) {
	router.HandleFunc("/previews/{message_id}/{preview_id}", previewHandler)
	router.HandleFunc("/attachments/{message_id}/{attachment_id}/{attachment_name}", attachmentHandler)
	router.HandleFunc("/external/{message_id}/{embed_id}", externalHandler)
	router.HandleFunc("/avatars/{user_id}/{modified}", avatarHandler)
}
