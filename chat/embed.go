package chat

import (
	. "clack/common"
	"clack/common/cache"
	"clack/common/snowflake"
	"clack/storage"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"zombiezen.com/go/sqlite"
)

const (
	MaxTitleLength       = 70
	MaxNameLength        = 50
	MaxDescriptionLength = 350
)

func MakeRequest(ctx context.Context, method string, targetURL string) (*http.Response, error) {
	if err := CheckURL(targetURL); err != nil {
		return nil, err
	}

	client := http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: CheckRedirect,
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.10; rv:38.0) Gecko/20100101 Firefox/38.0")

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("request timed out or cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	return resp, nil
}

type ContentInfo struct {
	Type     string
	Length   int64
	Filename string
	URL      string
}

func GetContentInfo(ctx context.Context, targetURL string) (*ContentInfo, error) {
	resp, err := MakeRequest(ctx, http.MethodHead, targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to make HEAD request: %w", err)
	}
	defer resp.Body.Close()

	info := &ContentInfo{
		Type:     "",
		Length:   0,
		Filename: "",
		URL:      targetURL,
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		mimeType, _, err := mime.ParseMediaType(contentType)
		if err == nil {
			info.Type = mimeType
		}
	}

	contentLengthStr := resp.Header.Get("Content-Length")

	if contentLengthStr != "" {
		info.Length, err = strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Content-Length: %w", err)
		}
	}

	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			info.Filename = params["filename"]
		}
	}

	if info.Filename == "" {
		if parsed, err := url.Parse(targetURL); err == nil {
			info.Filename = filepath.Base(parsed.Path)
		}
		if len(info.Filename) <= 1 {
			info.Filename = ""
		}
	}

	return info, nil
}

type responseReadCloser struct {
	io.Reader
	response *http.Response
}

func (r *responseReadCloser) Close() error {
	return r.response.Body.Close()
}

func GetContent(ctx context.Context, targetURL string) (*responseReadCloser, error) {
	resp, err := MakeRequest(ctx, http.MethodGet, targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to make GET request: %w", err)
	}

	return &responseReadCloser{
		Reader:   resp.Body,
		response: resp,
	}, nil
}

func GetContentString(ctx context.Context, targetURL string) (string, error) {
	resp, err := MakeRequest(ctx, http.MethodGet, targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to make GET request: %w", err)
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read content: %w", err)
	}

	return string(bytes), nil
}

type MediaInfo struct {
	Filename string
	Type     string
	Length   int64
	Data     []byte
}

type OEmbed struct {
	Type            string `json:"type"`
	Version         string `json:"version"`
	Title           string `json:"title,omitempty"`
	AuthorName      string `json:"author_name,omitempty"`
	AuthorURL       string `json:"author_url,omitempty"`
	ProviderName    string `json:"provider_name,omitempty"`
	ProviderURL     string `json:"provider_url,omitempty"`
	CacheAge        string `json:"cache_age,omitempty"`
	ThumbnailURL    string `json:"thumbnail_url,omitempty"`
	ThumbnailWidth  int    `json:"thumbnail_width,omitempty"`
	ThumbnailHeight int    `json:"thumbnail_height,omitempty"`

	Url    string `json:"url,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Html   string `json:"html,omitempty"`
}

func ParseOEmbed(ctx context.Context, embed *Embed, content string) error {
	var oembed OEmbed
	if err := json.Unmarshal([]byte(content), &oembed); err != nil {
		return fmt.Errorf("failed to unmarshal oembed: %w", err)
	}

	if oembed.Type == "rich" && oembed.Title != "" {
		embed.Title = oembed.Title
	}

	if oembed.ProviderName != "" {
		embed.Provider = &EmbedProvider{
			Name: oembed.ProviderName,
			URL:  oembed.ProviderURL,
		}
	}

	if oembed.AuthorName != "" {
		embed.Author = &EmbedAuthor{
			Name: oembed.AuthorName,
			URL:  oembed.AuthorURL,
		}
	}

	if oembed.ThumbnailURL != "" {
		embed.Thumbnail = &EmbedMedia{
			URL:    oembed.ThumbnailURL,
			Width:  oembed.ThumbnailWidth,
			Height: oembed.ThumbnailHeight,
		}
	}

	return nil
}

func ParseHexColor(hexColor string) int {
	if len(hexColor) < 6 {
		return 0
	}

	color, err := strconv.ParseInt(hexColor[1:], 16, 64)
	if err != nil {
		return 0
	}

	return int(color)
}

func GetRichEmbed(ctx context.Context, info *ContentInfo) (*Embed, error) {
	content, err := GetContentString(ctx, info.URL)

	fmt.Println(info.URL, content)

	if err != nil {
		return nil, fmt.Errorf("failed to get content: %w", err)
	}

	node, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	metaTags := make(map[string]string)
	oembedURLs := []string{}

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" {
			metaTags["title"] = n.FirstChild.Data
		}

		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, href, typ string
			for _, attr := range n.Attr {
				switch strings.ToLower(attr.Key) {
				case "rel":
					rel = attr.Val
				case "href":
					href = attr.Val
				case "type":
					typ = attr.Val
				}
			}

			if rel == "canonical" {
				metaTags["canonical"] = href
			} else if rel == "alternate" {
				if typ == "application/json+oembed" {
					oembedURLs = append(oembedURLs, href)
				}
			}
		}

		if n.Type == html.ElementNode && n.Data == "meta" {
			var name, property, content string
			for _, attr := range n.Attr {
				switch strings.ToLower(attr.Key) {
				case "name":
					name = attr.Val
				case "property":
					property = attr.Val
				case "content":
					content = attr.Val
				}
			}

			if name != "" {
				metaTags[name] = content
			} else if property != "" {
				metaTags[property] = content
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	traverse(node)

	embed := &Embed{
		Type: EmbedTypeRich,
	}

	if title, ok := metaTags["title"]; ok {
		embed.Title = title
	}

	if canonical, ok := metaTags["canonical"]; ok {
		embed.URL = canonical
	}

	if description, ok := metaTags["description"]; ok {
		embed.Description = description
	}

	if themeColor, ok := metaTags["theme-color"]; ok {
		embed.Color = ParseHexColor(themeColor)
	}

	if ogTitle, ok := metaTags["og:title"]; ok {
		embed.Title = ogTitle
	}

	if ogSiteName, ok := metaTags["og:site_name"]; ok {
		embed.Provider = &EmbedProvider{
			Name: ogSiteName,
		}
	}

	if ogURL, ok := metaTags["og:url"]; ok {
		embed.URL = ogURL
	}

	if ogDescription, ok := metaTags["og:description"]; ok {
		embed.Description = ogDescription
	}

	if ogImage, ok := metaTags["og:image"]; ok {
		embed.Image = &EmbedMedia{
			URL: ogImage,
		}
		if ogImageWidth, ok := metaTags["og:image:width"]; ok {
			if width, err := strconv.Atoi(ogImageWidth); err == nil {
				embed.Image.Width = width
			}
		}
		if ogImageHeight, ok := metaTags["og:image:height"]; ok {
			if height, err := strconv.Atoi(ogImageHeight); err == nil {
				embed.Image.Height = height
			}
		}

		ogType, _ := metaTags["og:type"]
		twitterCard, _ := metaTags["twitter:card"]

		shouldBeThumbnail := false

		if ogType == "website" || twitterCard == "summary" {
			shouldBeThumbnail = true
		}

		if twitterCard == "summary_large_image" {
			shouldBeThumbnail = false
		}

		if shouldBeThumbnail {
			embed.Thumbnail = embed.Image
			embed.Image = nil
		}
	}

	if ogVideo, ok := metaTags["og:video"]; ok {
		embed.Video = &EmbedMedia{
			URL: ogVideo,
		}
		if ogVideoWidth, ok := metaTags["og:video:width"]; ok {
			if width, err := strconv.Atoi(ogVideoWidth); err == nil {
				embed.Video.Width = width
			}
		}
		if ogVideoHeight, ok := metaTags["og:video:height"]; ok {
			if height, err := strconv.Atoi(ogVideoHeight); err == nil {
				embed.Video.Height = height
			}
		}
	}

	for _, oembedURL := range oembedURLs {
		oembedContent, err := GetContentString(ctx, oembedURL)
		if err != nil {
			continue
		}

		ParseOEmbed(ctx, embed, oembedContent)
	}

	return embed, nil
}

func GetImageEmbed(ctx context.Context, info *ContentInfo) (*Embed, error) {
	return &Embed{
		Type: EmbedTypeImage,
		Image: &EmbedMedia{
			URL: info.URL,
		},
	}, nil
}

func GetVideoEmbed(ctx context.Context, info *ContentInfo) (*Embed, error) {
	return &Embed{
		Type: EmbedTypeVideo,
		Video: &EmbedMedia{
			URL: info.URL,
		},
	}, nil
}

func GetImagePreviews(ctx context.Context, conn *sqlite.Conn, url string, id Snowflake) (width int, height int, err error) {
	content, err := GetContent(ctx, url)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get content: %w", err)
	}

	previews, err := storage.GetPreviews(content, false)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get previews: %w", err)
	}

	err = storage.AddPreviews(conn, id, previews)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to add previews: %w", err)
	}

	return previews.Width, previews.Height, nil
}

func GetVideoPreviews(ctx context.Context, conn *sqlite.Conn, url string, contentType string, length int64, id Snowflake) (width int, height int, err error) {
	content, err := GetContent(ctx, url)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get content: %w", err)
	}

	cache, err := cache.GetCacheWriter(url, contentType, 0, length)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get cache writer: %w", err)
	}

	reader := io.TeeReader(content, cache)

	previews, err := storage.GetPreviews(reader, false)

	cache.Close()
	content.Close()

	if err != nil {
		return 0, 0, fmt.Errorf("failed to get previews: %w", err)
	}

	err = storage.AddPreviews(conn, id, previews)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to add previews: %w", err)
	}

	return previews.Width, previews.Height, nil
}

func GetEmbedFromURL(ctx context.Context, conn *sqlite.Conn, targetURL string) (embed *Embed, err error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	info, err := GetContentInfo(timeoutCtx, targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get content info: %w", err)
	}

	if info.Length > int64(MaxContentLength) {
		return nil, fmt.Errorf("content too large: %d bytes", info.Length)
	}

	if info.Type == "text/html" {
		embed, err = GetRichEmbed(timeoutCtx, info)
	} else if slices.Contains(SupportedImageTypes, info.Type) {
		embed, err = GetImageEmbed(timeoutCtx, info)
	} else if slices.Contains(SupportedVideoTypes, info.Type) {
		embed, err = GetVideoEmbed(timeoutCtx, info)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get embed: %w", err)
	}

	haveVideo := false

	if embed.Video != nil {
		var previewURL string = embed.Video.URL

		if embed.Image != nil {
			previewURL = embed.Image.URL
		}

		if embed.Thumbnail != nil {
			previewURL = embed.Thumbnail.URL
		}

		embed.Video.ID = snowflake.New()

		width, height, err := GetVideoPreviews(ctx, conn, previewURL, info.Type, info.Length, embed.Video.ID)

		if err == nil {
			embed.Video.Width = width
			embed.Video.Height = height
			haveVideo = true
		}
	}

	if haveVideo {
		embed.Image = nil
		embed.Thumbnail = nil
	} else {
		embed.Video = nil
	}

	haveImage := false
	if embed.Image != nil {
		var previewURL string = embed.Image.URL

		if embed.Thumbnail != nil {
			previewURL = embed.Thumbnail.URL
		}

		embed.Image.ID = snowflake.New()

		width, height, err := GetImagePreviews(ctx, conn, previewURL, embed.Image.ID)

		if err == nil {
			embed.Image.Width = width
			embed.Image.Height = height
			haveImage = true
		}
	}

	if haveImage {
		embed.Video = nil
		embed.Thumbnail = nil
	} else {
		embed.Image = nil
	}

	haveThumbnail := false
	if embed.Thumbnail != nil {

		embed.Thumbnail.ID = snowflake.New()

		width, height, err := GetImagePreviews(ctx, conn, embed.Thumbnail.URL, embed.Thumbnail.ID)

		if err == nil {
			embed.Thumbnail.Width = width
			embed.Thumbnail.Height = height
			haveThumbnail = true
		}
	}

	if haveThumbnail {
		embed.Video = nil
		embed.Image = nil
	} else {
		embed.Thumbnail = nil
	}

	var uselessDesc = false

	uselessDesc = uselessDesc || (embed.Provider != nil && embed.Description == embed.Provider.Name)
	uselessDesc = uselessDesc || (embed.Author != nil && embed.Description == embed.Author.Name)
	uselessDesc = uselessDesc || (embed.Title != "" && embed.Description == embed.Title)

	if uselessDesc {
		embed.Description = ""
	}

	if embed.Author != nil && embed.Author.Name != "" {
		embed.Author.Name = TruncateText(embed.Author.Name, MaxNameLength, true)
	}

	if embed.Provider != nil && embed.Provider.Name != "" {
		embed.Provider.Name = TruncateText(embed.Provider.Name, MaxNameLength, true)
	}

	if embed.Title != "" {
		embed.Title = TruncateText(embed.Title, MaxTitleLength, true)
	}

	if embed.Description != "" {
		embed.Description = TruncateText(embed.Description, MaxDescriptionLength, false)
	}

	if embed.Footer != nil && embed.Footer.Text != "" {
		embed.Footer.Text = TruncateText(embed.Footer.Text, MaxTitleLength, true)
	}

	return embed, nil // Unknown content type, or not handled embed type
}
