package storage

import (
	"bytes"
	. "clack/common"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/gabriel-vasile/mimetype"
)

func GetAttachmentPath(messageID Snowflake, attachmentID Snowflake) string {
	return fmt.Sprintf("attachments/%d/%d", messageID, attachmentID)
}

func GetPreviewPath(messageID Snowflake, previewID Snowflake, size string) string {
	return fmt.Sprintf("previews/%d/%d/%s", messageID, previewID, size)
}

func WriteFile(path string, input FileInputReader) error {
	file := filepath.Join(DataFolder, path)
	os.MkdirAll(filepath.Dir(file), 0755)

	disk, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer disk.Close()

	_, err = io.Copy(disk, input)
	if err != nil {
		os.Remove(file)
		os.RemoveAll(filepath.Dir(file))
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func ReadFile(path string) (FileOutputReader, error) {
	file := filepath.Join(DataFolder, path)
	disk, err := os.Open(file)
	if err == nil {
		_, err := disk.Stat()
		if err != nil {
			disk.Close()
			return nil, NewError(ErrorCodeInternalError, fmt.Errorf("failed to stat file: %w", err))
		}
		return &DiskReader{File: disk}, nil
	}
	return nil, ErrFileNotFound
}

func UploadAttachment(messageID Snowflake, attachmentID Snowflake, filename string, input FileInputReader) (*Attachment, error) {
	path := GetAttachmentPath(messageID, attachmentID)

	err := WriteFile(path, input)
	if err != nil {
		return nil, err
	}

	file, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	id := attachmentID
	typ := AttachmentTypeFile

	file.Seek(0, io.SeekStart)
	mime, _ := mimetype.DetectReader(file)

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

	attachment := Attachment{
		ID:       id,
		Filename: filename,
		Type:     typ,
		MimeType: mimeType,
		Size:     int(file.Size()),
	}

	var previews *Previews = nil
	if typ != AttachmentTypeFile {
		file.Seek(0, io.SeekStart)
		previews, err = CreatePreviews(file, false)
		if typ == AttachmentTypeImage && mimeType == "image/gif" {
			file.Seek(0, io.SeekStart)
			CreateAnimatedPreview(file, previews)
		}
		if err != nil {
			fmt.Println("Failed to generate previews:", err)
			typ = AttachmentTypeFile
		} else {
			attachment.Width = previews.Width
			attachment.Height = previews.Height
		}
	}

	if previews != nil {
		attachment.Preload, err = WritePreviews(messageID, id, previews)
		if err != nil {
			return nil, NewError(ErrorCodeInternalError, fmt.Errorf("failed to write previews: %w", err))
		}
	}

	return &attachment, nil
}

func ReadAttachment(messageID Snowflake, attachmentID Snowflake) (FileOutputReader, error) {
	path := GetAttachmentPath(messageID, attachmentID)
	file, err := ReadFile(path)
	if err != nil {
		return nil, err
	}

	return file, nil
}

func GetWebpPreload(data []byte) string {
	return "data:image/webp;base64, " + base64.StdEncoding.EncodeToString(data)
}

func WritePreviews(messageID Snowflake, previewID Snowflake, previews *Previews) (string, error) {
	displayPath := GetPreviewPath(messageID, previewID, "display")
	err := WriteFile(displayPath, bytes.NewReader(previews.Display))
	if err != nil {
		return "", NewError(ErrorCodeInternalError, fmt.Errorf("failed to write display preview: %w", err))
	}

	previewPath := GetPreviewPath(messageID, previewID, "thumbnail")
	err = WriteFile(previewPath, bytes.NewReader(previews.Thumb))
	if err != nil {
		return "", NewError(ErrorCodeInternalError, fmt.Errorf("failed to write preview: %w", err))
	}

	preload := GetWebpPreload(previews.Preload)
	return preload, nil
}

func GetFile(name string) (*File, error) {
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

func GetPreview(messageID Snowflake, previewID Snowflake, typ string) (*File, error) {
	name := GetPreviewPath(messageID, previewID, typ)
	file, err := GetFile(name)

	if err != nil {
		return nil, err
	}

	file.Mimetype = "image/webp"
	return file, nil
}

func GetAttachment(messageID Snowflake, attachment Attachment) (*File, error) {
	path := GetAttachmentPath(messageID, attachment.ID)

	file, err := GetFile(path)
	if err != nil {
		return nil, err
	}

	file.Mimetype = attachment.MimeType
	return file, nil
}
