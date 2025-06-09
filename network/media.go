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
	"time"

	"github.com/gorilla/mux"
)

func attachmentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	attachmentID, err := strconv.ParseInt(vars["attachment_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid attachment id", http.StatusBadRequest)
		return
	}

	attachmentName := vars["attachment_name"]

	db, err := storage.OpenDatabase(context.Background())
	defer storage.CloseDatabase(db)

	if err != nil {
		srvLog.Printf("Failed to open database: %v", err)
		http.Error(w, "failed to open database", http.StatusInternalServerError)
		return
	}

	attch, err := storage.GetAttachment(db, snowflake.Snowflake(attachmentID), attachmentName)
	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			http.Error(w, "attachment not found", http.StatusNotFound)
		} else {
			srvLog.Printf("Failed to get attachment (ID: %d, Name: %s): %v", attachmentID, attachmentName, err)
			http.Error(w, "failed to get attachment", http.StatusInternalServerError)
		}
		return
	}
	attch.Modified = time.Now()

	w.Header().Set("Content-Type", attch.Mimetype)
	w.Header().Set("Content-Disposition", "inline; filename="+attch.Name)

	http.ServeContent(w, r, attch.Name, attch.Modified, attch.Content)

	attch.Content.Close()
}

func previewHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	previewID, err := strconv.ParseInt(vars["preview_id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid preview id", http.StatusBadRequest)
		return
	}

	previewType := r.URL.Query().Get("type")
	if previewType == "" || (previewType != "preview" && previewType != "display" && previewType != "blur") {
		http.Error(w, "invalid preview type", http.StatusBadRequest)
		return
	}

	db, err := storage.OpenDatabase(context.Background())
	defer storage.CloseDatabase(db)

	if err != nil {
		srvLog.Printf("Failed to open database: %v", err)
		http.Error(w, "failed to open database", http.StatusInternalServerError)
		return
	}

	preview, err := storage.GetPreview(db, snowflake.Snowflake(previewID), previewType)

	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			http.Error(w, "preview not found", http.StatusNotFound)
		} else {
			srvLog.Printf("Failed to get preview (ID: %d): %v", previewID, err)
			http.Error(w, "failed to get preview", http.StatusInternalServerError)
		}
		return
	}

	preview.Modified = time.Now()

	w.Header().Set("Content-Type", preview.Mimetype)

	http.ServeContent(w, r, preview.Name, preview.Modified, preview.Content)

	preview.Content.Close()
}

func externalHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	targetEmbedID, err := strconv.ParseInt(vars["embed_id"], 10, 64)
	if err != nil {
		srvLog.Printf("Invalid embed id: %v", err)
		http.Error(w, "invalid embed id", http.StatusBadRequest)
		return
	}

	targetURLEncoded := r.URL.Query().Get("url")
	if targetURLEncoded == "" {
		srvLog.Printf("Missing url parameter")
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	targetURL := targetURLEncoded /*, err := url.QueryUnescape(targetURLEncoded)
	if err != nil {
		srvLog.Printf("Failed to unescape URL: %v", err)
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}*/

	db, err := storage.OpenDatabase(context.Background())
	defer storage.CloseDatabase(db)

	allowed, err := storage.IsURLAllowed(db, snowflake.Snowflake(targetEmbedID), targetURL)
	if err != nil {
		srvLog.Printf("Failed to check URL: %v", err)
		http.Error(w, "failed to check URL", http.StatusInternalServerError)
		return
	}

	if !allowed {
		fmt.Println("URL not allowed:", targetURLEncoded, targetURL)
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

	err = cache.ServeExternal(w, r, targetURL)
	if err != nil {
		if strings.HasPrefix(err.Error(), "origin") {
			http.Error(w, err.Error(), http.StatusBadGateway)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func buildMediaRouter(router *mux.Router) {
	router.HandleFunc("/previews/{preview_id}", previewHandler)
	router.HandleFunc("/attachments/{attachment_id}/{attachment_name}", attachmentHandler)
	router.HandleFunc("/external/{embed_id}", externalHandler)
}
