package storage

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

type Previews struct {
	Width   int
	Height  int
	Display []byte
	Preview []byte
	Blur    []byte
}

func GetOriginal(content io.Reader, useTemp bool) ([]byte, error) {
	commonArgs := []string{
		"-threads", "1",
		"-i", "-",
		"-vframes", "1",
		"-quality", "100",
		"-c:v", "libwebp",
		"-f", "image2pipe",
		"-",
	}

	if useTemp {
		tmp, err := os.CreateTemp("", "ffmpeg-")
		if err != nil {
			return nil, fmt.Errorf("create temp file: %v", err)
		}
		tmpPath := tmp.Name()
		_, err = io.Copy(tmp, content)
		if err != nil {
			return nil, fmt.Errorf("copy to temp file: %v", err)
		}
		commonArgs[1] = tmpPath
		return runFFmpegOnTmpFile(commonArgs, tmpPath)

	} else {
		return runFFmpegOnReader(commonArgs, content)
	}
}

func GetPreviews(content io.Reader, useTemp bool) (*Previews, error) {
	original, err := GetOriginal(content, useTemp)
	if err != nil {
		return nil, fmt.Errorf("GetOriginal: %v", err)
	}

	var p Previews

	probeArgs := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,format_name",
		"-of", "default=nw=1:nk=1",
		"-",
	}

	probeOutput, err := runFFprobeOnReader(probeArgs, bytes.NewReader(original))
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %v", err)
	}

	_, err = fmt.Sscanf(probeOutput, "%d\n%d", &p.Width, &p.Height)
	if err != nil {
		return nil, fmt.Errorf("parse dimensions: %v", err)
	}

	commonArgs := []string{
		"-i", "-",
		"-vframes", "1",
		"-quality", "100",
		"-c:v", "libwebp",
		"-f", "image2pipe",
	}

	displayArgs := append(commonArgs,
		"-vf", "scale=w='min(iw,1200)':h='min(ih,1200)':force_original_aspect_ratio=decrease", "-",
	)

	p.Display, err = runFFmpegOnReader(displayArgs, bytes.NewReader(original))
	if err != nil {
		return nil, fmt.Errorf("get display: %v", err)
	}

	previewArgs := append(commonArgs,
		"-vf", "scale=w='min(iw,350)':h='min(ih,350)':force_original_aspect_ratio=increase", "-",
	)

	p.Preview, err = runFFmpegOnReader(previewArgs, bytes.NewReader(p.Display))
	if err != nil {
		return nil, fmt.Errorf("get preview: %v", err)
	}

	blurArgs := append(commonArgs,
		"-vf", "huesaturation=intensity=1,boxblur=32,scale=w='min(iw,32)':h='min(ih,32)':force_original_aspect_ratio=decrease", "-",
	)

	p.Blur, err = runFFmpegOnReader(blurArgs, bytes.NewReader(p.Preview))
	if err != nil {
		return nil, fmt.Errorf("get blur: %v", err)
	}

	return &p, nil
}
