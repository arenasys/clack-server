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
	Thumb   []byte
	Preload []byte
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

func GetDimensions(content io.Reader, p *Previews) error {
	probeArgs := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,format_name",
		"-of", "default=nw=1:nk=1",
		"-",
	}

	probeOutput, err := runFFprobeOnReader(probeArgs, content)
	if err != nil {
		return fmt.Errorf("ffprobe: %v", err)
	}

	var width int
	var height int

	_, err = fmt.Sscanf(probeOutput, "%d\n%d", &width, &height)
	if err != nil {
		return fmt.Errorf("parse dimensions: %v", err)
	}

	p.Width = width
	p.Height = height

	return nil
}

func CreateAnimatedPreview(content io.ReadSeeker, p *Previews) error {
	animatedArgs := []string{
		"-i", "-",
		"-c:v", "libwebp_anim",
		"-loop", "0",
		"-f", "image2pipe",
		"-vf", "scale=w='min(iw,550)':h='min(ih,550)':force_original_aspect_ratio=increase",
		"-",
	}

	animatedData, err := runFFmpegOnReader(animatedArgs, content)
	if err != nil {
		return fmt.Errorf("get animated preview: %v", err)
	}

	p.Display = animatedData
	return nil
}

func CreatePreviews(content io.Reader, useTemp bool) (*Previews, error) {
	original, err := GetOriginal(content, useTemp)
	if err != nil {
		return nil, fmt.Errorf("GetOriginal: %v", err)
	}

	var p Previews

	err = GetDimensions(bytes.NewReader(original), &p)
	if err != nil {
		return nil, fmt.Errorf("get dimensions: %v", err)
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

	thumbArgs := append(commonArgs,
		"-vf", "scale=w='min(iw,350)':h='min(ih,350)':force_original_aspect_ratio=increase", "-",
	)

	p.Thumb, err = runFFmpegOnReader(thumbArgs, bytes.NewReader(p.Display))
	if err != nil {
		return nil, fmt.Errorf("get thumb: %v", err)
	}

	preloadArgs := append(commonArgs,
		"-vf", "huesaturation=intensity=1,boxblur=32,scale=w='min(iw,32)':h='min(ih,32)':force_original_aspect_ratio=decrease", "-",
	)

	p.Preload, err = runFFmpegOnReader(preloadArgs, bytes.NewReader(p.Thumb))
	if err != nil {
		return nil, fmt.Errorf("get preload: %v", err)
	}

	return &p, nil
}
