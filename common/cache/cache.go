package cache

import (
	. "clack/common"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

var cacheLog = NewLogger("CACHE")

var cacheLocks sync.Map

func LogRangeHumanReadable(prefix string, start int64, end int64, total int64) {
	if start < 0 || end < 0 || total <= 0 {
		fmt.Printf("%s: UNKNOWN - UNKNOWN\n", prefix)
		return
	}

	startf, endf, totalf := float64(start), float64(end), float64(total)
	fmt.Printf("%s: %.2f%% - %.2f%%\n", prefix, (100 * startf / totalf), (100 * endf / totalf))
}

type CacheMiss struct {
	io.ReadCloser
	start               int64
	end                 int64
	total               int64
	progress            int64
	progressSinceUpdate int64
	cacheFile           *os.File
	metaFilePath        string
	lock                *sync.RWMutex
	contentType         string
}

func (c *CacheMiss) Read(p []byte) (int, error) {
	c.cacheFile.Seek(c.start+c.progress, 0)

	n, err := c.ReadCloser.Read(p)

	c.cacheFile.Write(p[:n])

	c.progress += int64(n)
	c.progressSinceUpdate += int64(n)

	if c.progressSinceUpdate > 1024*1024 {
		c.lock.Lock()
		UpdateCacheMetadata(c.metaFilePath, c.contentType, c.total, &CacheRangeSpec{
			Start: c.start,
			End:   c.start + c.progress - 1,
		})
		c.lock.Unlock()
		c.progressSinceUpdate = 0
	}

	return n, err
}

func (c *CacheMiss) Close() error {
	c.cacheFile.Close()

	start, end, total := c.start, c.start+c.progress-1, c.total

	c.lock.Lock()
	err := UpdateCacheMetadata(c.metaFilePath, c.contentType, total, &CacheRangeSpec{
		Start: start,
		End:   end,
	})
	c.lock.Unlock()

	if err == nil {
		LogRangeHumanReadable("CacheWrite", start, end, total)
	}

	return c.ReadCloser.Close()
}

type CacheHit struct {
	io.Reader
	file *os.File
}

func (c *CacheHit) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	return n, err
}

func (c *CacheHit) Close() error {
	c.file.Close()
	return nil
}

type CacheRangeSpec struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type CacheMetadata struct {
	Type   string           `json:"type"`
	Length int64            `json:"length"`
	Ranges []CacheRangeSpec `json:"ranges"`
}

func GetCacheHash(url string) string {
	md5sum := md5.New()
	md5sum.Write([]byte(url))
	return fmt.Sprintf("%x", md5sum.Sum(nil))
}

func GetCacheLock(cacheHash string) *sync.RWMutex {
	lock, _ := cacheLocks.LoadOrStore(cacheHash, &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

func GetCacheMetadata(path string) (*CacheMetadata, error) {
	cacheMetadataFile, err := os.OpenFile(path, os.O_RDONLY, 0666)
	if err != nil {
		if !os.IsNotExist(err) {
			cacheLog.Printf("Failed to open cache metadata file: %v", err)
		}
		return nil, fmt.Errorf("failed to open cache metadata file: %v", err)
	}
	defer cacheMetadataFile.Close()

	var cacheMetadata CacheMetadata
	err = json.NewDecoder(cacheMetadataFile).Decode(&cacheMetadata)
	if err != nil {
		cacheLog.Printf("Failed to decode cache metadata: %v", err)
		return nil, fmt.Errorf("failed to decode cache metadata: %v", err)
	}
	return &cacheMetadata, nil
}

func UpdateCacheMetadata(path string, contentType string, length int64, newRange *CacheRangeSpec) error {

	metadata, err := GetCacheMetadata(path)
	if err != nil {
		metadata = &CacheMetadata{
			Type:   contentType,
			Length: length,
			Ranges: []CacheRangeSpec{},
		}
	}

	if newRange != nil {
		metadata.Ranges = append(metadata.Ranges, *newRange)
	}

	metadata.Ranges = MergeRanges(metadata.Ranges)

	fmt.Println("CacheUpdate:")
	for _, cacheRange := range metadata.Ranges {
		LogRangeHumanReadable("  ", cacheRange.Start, cacheRange.End, metadata.Length)
	}

	cacheMetadataFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		cacheLog.Printf("Failed to open or create cache metadata file: %v", err)
		return err
	}
	defer cacheMetadataFile.Close()

	cacheMetadataFile.Truncate(0)
	err = json.NewEncoder(cacheMetadataFile).Encode(metadata)
	if err != nil {
		cacheLog.Printf("Failed to encode cache metadata: %v", err)
		return err
	}

	return nil
}

func MergeRanges(ranges []CacheRangeSpec) []CacheRangeSpec {
	if len(ranges) == 0 {
		return nil
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	merged := []CacheRangeSpec{}
	current := ranges[0]

	for _, r := range ranges[1:] {
		if r.Start <= current.End+1 {
			if r.End > current.End {
				current.End = r.End
			}
		} else {
			merged = append(merged, current)
			current = r
		}
	}

	merged = append(merged, current)

	return merged
}

func GetCachedRange(cacheMetadata *CacheMetadata, reqRange CacheRangeSpec) (bool, CacheRangeSpec) {
	// if needed, truncates the range to span entirely cached or uncached data

	if reqRange.End == -1 {
		reqRange.End = cacheMetadata.Length - 1
	}

	for _, cacheRange := range cacheMetadata.Ranges {
		if reqRange.End < cacheRange.Start {
			// complete miss
			return false, CacheRangeSpec{
				Start: reqRange.Start, End: reqRange.End,
			}
		} else if reqRange.End >= cacheRange.Start && reqRange.Start < cacheRange.Start {
			// partial miss, partial hit. return partial miss
			return false, CacheRangeSpec{
				Start: reqRange.Start, End: cacheRange.Start - 1,
			}
		} else if reqRange.End <= cacheRange.End && reqRange.Start >= cacheRange.Start {
			// complete hit
			return true, CacheRangeSpec{
				Start: reqRange.Start, End: reqRange.End,
			}
		} else if reqRange.End > cacheRange.End && reqRange.Start <= cacheRange.End {
			// partial hit, partial miss. return partial hit
			return true, CacheRangeSpec{
				Start: reqRange.Start, End: cacheRange.End,
			}
		}
	}

	return false, reqRange
}

func ParseRangeHeader(rangeHeader string) (CacheRangeSpec, error) {
	var s CacheRangeSpec = CacheRangeSpec{
		Start: 0,
		End:   -1,
	}

	if n, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &s.Start, &s.End); err == nil && n == 2 {
		return s, nil
	} else if n, err = fmt.Sscanf(rangeHeader, "bytes=%d-", &s.Start); err == nil && n == 1 {
		return s, nil
	} else {
		return s, fmt.Errorf("invalid range format")
	}
}

func MakeProxyRequest(ctx context.Context, req *http.Request, targetURL string) (*http.Response, error) {
	if err := CheckURL(targetURL); err != nil {
		return nil, err
	}

	client := http.Client{
		CheckRedirect: CheckRedirect,
	}

	proxyReq, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	proxyReq.Header.Set("User-Agent", UserAgent)

	if req.Header.Get("Range") != "" {
		proxyReq.Header.Set("Range", req.Header.Get("Range"))
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("request timed out or cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

type cacheRequest struct {
	url           string
	hash          string
	lock          *sync.RWMutex
	cacheFilePath string
	metaFilePath  string
}

func TryServeCached(w http.ResponseWriter, r *http.Request, info cacheRequest) error {
	fmt.Println("TRY SERVE CACHED")
	if _, err := os.Stat(info.metaFilePath); err != nil {
		return err
	}

	var requestRange CacheRangeSpec
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var err error
		requestRange, err = ParseRangeHeader(rangeHeader)
		if err != nil {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return err
		}
	} else {
		requestRange = CacheRangeSpec{
			Start: 0,
			End:   -1,
		}
	}

	info.lock.RLock()
	metadata, err := GetCacheMetadata(info.metaFilePath)
	info.lock.RUnlock()

	if err != nil {
		cacheLog.Printf("Failed to get cache metadata: %v", err)
		return err
	}

	hit, cachedRange := GetCachedRange(metadata, requestRange)

	if !hit {
		return fmt.Errorf("request range not in cache: %v", requestRange)
	}

	w.Header().Set("Content-Type", metadata.Type)
	if r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cachedRange.Start, cachedRange.End, metadata.Length))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", cachedRange.End-cachedRange.Start+1))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", metadata.Length))
		w.WriteHeader(http.StatusOK)
	}

	cacheFile, err := os.OpenFile(info.cacheFilePath, os.O_RDONLY, 0)
	if err != nil {
		cacheLog.Printf("Failed to open cache file: %v", err)
		return err
	}

	//start, end, total := cachedRange.Start, cachedRange.End, metadata.Length

	//LogRangeHumanReadable("CacheHit", start, end, total)

	cacheHit := io.NewSectionReader(cacheFile, cachedRange.Start, cachedRange.End-cachedRange.Start+1)

	io.Copy(w, cacheHit)

	cacheFile.Close()
	return nil
}

func TryServeUncached(ctx context.Context, w http.ResponseWriter, r *http.Request, info cacheRequest) error {
	proxyResp, err := MakeProxyRequest(ctx, r, info.url)
	if err != nil {
		cacheLog.Printf("Failed to make request: %v", err)
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer proxyResp.Body.Close()

	if proxyResp.StatusCode != http.StatusOK && proxyResp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("origin returned: %d", proxyResp.StatusCode)
	}

	if proxyResp.StatusCode == http.StatusPartialContent && proxyResp.Header.Get("Content-Range") == "" {
		return fmt.Errorf("origin sent no content range")
	}

	if proxyResp.Header.Get("Content-Type") == "" {
		return fmt.Errorf("origin sent no content type")
	}

	contentType := proxyResp.Header.Get("Content-Type")
	w.Header().Set("Content-Type", contentType)

	var start, end, total int64 = 0, -1, -1
	if proxyResp.StatusCode == http.StatusPartialContent {
		_, err = fmt.Sscanf(proxyResp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total)
		if err != nil {
			return err
		}
		contentType = proxyResp.Header.Get("Content-Type")

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
	} else if proxyResp.StatusCode == http.StatusOK {
		if proxyResp.ContentLength >= 0 {
			start, end, total = 0, proxyResp.ContentLength, proxyResp.ContentLength
			w.Header().Set("Content-Length", fmt.Sprintf("%d", total))
		}
		w.WriteHeader(http.StatusOK)
	} else {
		return fmt.Errorf("invalid response status: %d", proxyResp.StatusCode)
	}

	if total < 0 {
		fmt.Println("CacheBypass")
		io.Copy(w, proxyResp.Body)
	} else {

		LogRangeHumanReadable("CacheMiss", start, end, total)
		os.MkdirAll(filepath.Dir(info.cacheFilePath), 0755)

		cacheFile, err := os.OpenFile(info.cacheFilePath, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			cacheLog.Printf("Failed to open or create cache file: %v", err)
			return err
		}

		//cacheFile.Truncate(total)

		cacheMiss := &CacheMiss{
			ReadCloser:   proxyResp.Body,
			start:        start,
			end:          end,
			total:        total,
			progress:     0,
			cacheFile:    cacheFile,
			metaFilePath: info.metaFilePath,
			lock:         info.lock,
			contentType:  contentType,
		}

		io.Copy(w, cacheMiss)

		cacheMiss.Close()
	}
	return nil
}

func GetCacheRequest(url string) cacheRequest {
	hash := GetCacheHash(url)
	lock := GetCacheLock(hash)
	filePath := filepath.Join(DataFolder, "external", hash)

	return cacheRequest{
		url:           url,
		hash:          hash,
		lock:          lock,
		cacheFilePath: filePath,
		metaFilePath:  filePath + ".meta",
	}
}

func ServeExternal(w http.ResponseWriter, r *http.Request, url string) error {
	info := GetCacheRequest(url)

	err := TryServeCached(w, r, info)
	if err != nil {
		err = TryServeUncached(r.Context(), w, r, info)
		if err != nil {
			cacheLog.Printf("Failed to serve external content: %v", err)
			return err
		}
	}
	return nil
}

type CacheWriter struct {
	os.File
	contentType string
	start       int64
	progress    int64
	total       int64
	info        *cacheRequest
}

func (c *CacheWriter) Write(p []byte) (int, error) {
	n, err := c.File.Write(p)
	if err == nil {
		c.progress += int64(n)
	}
	return n, err
}

func (c *CacheWriter) Close() error {
	c.File.Close()

	length := c.start + c.progress
	end := length - 1

	c.info.lock.Lock()
	err := UpdateCacheMetadata(c.info.metaFilePath, c.contentType, c.total, &CacheRangeSpec{
		Start: c.start,
		End:   end,
	})
	c.info.lock.Unlock()

	if err == nil {
		LogRangeHumanReadable("InitialCacheWrite", c.start, end, c.total)
	} else {
		cacheLog.Printf("Failed to update cache metadata: %v", err)
	}

	return err
}

func GetCacheWriter(url string, contentType string, start int64, total int64) (*CacheWriter, error) {
	info := GetCacheRequest(url)

	os.MkdirAll(filepath.Dir(info.cacheFilePath), 0755)

	cacheFile, err := os.OpenFile(info.cacheFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		cacheLog.Printf("Failed to open or create cache file: %v", err)
		return nil, err
	}

	cacheFile.Seek(start, 0)

	return &CacheWriter{
		File:        *cacheFile,
		contentType: contentType,
		start:       start,
		progress:    0,
		total:       total,
		info:        &info,
	}, nil
}
