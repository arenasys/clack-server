package common

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
	"unicode"
)

type ClackContext struct {
	context.Context
	Cancel     context.CancelFunc
	Subsystems sync.WaitGroup
}

type CodedError struct {
	Code    int
	Message string
}

func (e *CodedError) Error() string {
	return e.Message
}

func NewError(code int, err error) *CodedError {
	msg := ""
	if err != nil {
		msg = err.Error()
	}

	return &CodedError{
		Code:    code,
		Message: msg,
	}
}

var (
	SupportedImageTypes = []string{
		"image/jpeg",
		"image/png",
		"image/gif",
		"image/webp",
		"image/svg+xml",
	}

	SupportedVideoTypes = []string{
		"video/mp4",
		"video/webm",
		"video/x-matroska",
	}

	DataFolder = "data"

	MaxContentLength    = int64(1024 * 1024 * 64) // 64MB
	MaxDatabaseFileSize = int64(1024 * 1024)      // 1MB

	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.10; rv:38.0) Gecko/20100101 Firefox/38.0"
)

type File struct {
	Name     string
	Size     int
	Mimetype string
	Modified time.Time
	Content  FileOutputReader
}

type FileInputReader interface {
	io.Reader
	Size() int64
}

type FileOutputReader interface {
	io.ReadSeekCloser
	Size() int64
}

type DiskReader struct {
	File *os.File
}

func (r *DiskReader) Read(p []byte) (int, error) {
	return r.File.Read(p)
}

func (r *DiskReader) Seek(offset int64, whence int) (int64, error) {
	return r.File.Seek(offset, whence)
}

func (r *DiskReader) Close() error {
	return r.File.Close()
}

func (r *DiskReader) Size() int64 {
	stat, err := r.File.Stat()
	if err != nil {
		panic(err)
	}
	return stat.Size()
}

type PartReader struct {
	reader io.Reader
	part   *multipart.Part
	size   int64
}

func (r *PartReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *PartReader) Close() error {
	return r.part.Close()
}

func (r *PartReader) Size() int64 {
	return r.size
}

func NewPartReader(part *multipart.Part, size int64) *PartReader {
	return &PartReader{
		reader: io.LimitReader(part, size),
		part:   part,
		size:   size,
	}
}

func TruncateText(text string, length int, breakWords bool) string {
	const wordBoundryTolerance = 10

	if length <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= length {
		return text
	}

	truncateIndex := length
	if !breakWords {
		for i := length; i > length-wordBoundryTolerance && i > 0; i-- {
			if unicode.IsSpace(runes[i-1]) || unicode.IsPunct(runes[i-1]) {
				truncateIndex = i - 1
				break
			}
		}
	}

	truncated := string(runes[:truncateIndex]) + "..."
	return truncated
}

func CheckURL(targetURL string) error {
	parsedUrl, err := url.Parse(targetURL)
	if err != nil || parsedUrl.Scheme == "" || parsedUrl.Host == "" || (parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https") {
		return fmt.Errorf("invalid URL: %s", targetURL)
	}
	return nil
}

func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func NewLogger(prefix string) *log.Logger {
	prefix = fmt.Sprintf("[%s] ", prefix)
	return log.New(os.Stdout, prefix, 0)
}

func HashSha256(data string, salt string) string {
	var hash = sha256.Sum256([]byte(data + salt))
	return hex.EncodeToString(hash[:])
}

func HashCRC32(data string) uint32 {
	return crc32.ChecksumIEEE([]byte(data))
}

func GetRandom128() string {
	return rand.Text()
}

func GetRandom256() string {
	return GetRandom128() + GetRandom128()
}
