package storage

import (
	"bytes"
	"fmt"
	"io"
)

type Previews struct {
	Width   int
	Height  int
	Display []byte
	Thumb   []byte
	Preload []byte
}

func GetOriginal(stream io.Reader, path string) ([]byte, error) {
	args := []string{
		"-threads", "1",
		"-i", "",
		"-vframes", "1",
		"-quality", "100",
		"-c:v", "libwebp",
		"-f", "image2pipe",
		"-",
	}

	if path != "" {
		args[3] = path
		return runFFmpegOnFile(args, path)
	} else {
		args[3] = "-"
		return runFFmpegOnStream(args, stream)
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

	probeOutput, err := runFFprobeOnStream(probeArgs, content)
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

func CreatePreviews(stream io.Reader, path string) (*Previews, error) {
	original, err := GetOriginal(stream, path)
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

	p.Display, err = runFFmpegOnStream(displayArgs, bytes.NewReader(original))
	if err != nil {
		return nil, fmt.Errorf("get display: %v", err)
	}

	thumbArgs := append(commonArgs,
		"-vf", "scale=w='min(iw,350)':h='min(ih,350)':force_original_aspect_ratio=increase", "-",
	)

	p.Thumb, err = runFFmpegOnStream(thumbArgs, bytes.NewReader(p.Display))
	if err != nil {
		return nil, fmt.Errorf("get thumb: %v", err)
	}

	preloadArgs := append(commonArgs,
		"-vf", "huesaturation=intensity=1,boxblur=32,scale=w='min(iw,32)':h='min(ih,32)':force_original_aspect_ratio=decrease", "-",
	)

	p.Preload, err = runFFmpegOnStream(preloadArgs, bytes.NewReader(p.Thumb))
	if err != nil {
		return nil, fmt.Errorf("get preload: %v", err)
	}

	return &p, nil
}

func CreateAnimatedPreview(stream io.Reader, path string) ([]byte, error) {
	args := []string{
		"-i", "",
		"-c:v", "libwebp_anim",
		"-loop", "0",
		"-f", "image2pipe",
		"-vf", "scale=w='min(iw,550)':h='min(ih,550)':force_original_aspect_ratio=increase",
		"-",
	}

	if path != "" {
		args[1] = path
		return runFFmpegOnFile(args, path)
	} else {
		args[1] = "-"
		return runFFmpegOnStream(args, stream)
	}
}

type Avatar struct {
	Display []byte
	Thumb   []byte
}

func CreateAvatar(content io.Reader) (*Avatar, error) {
	var a Avatar
	var err error

	commonArgs := []string{
		"-threads", "1",
		"-i", "-",
		"-vframes", "1",
		"-quality", "100",
		"-compression_level", "6",
		"-c:v", "libwebp",
		"-f", "image2pipe",
	}

	displayArgs := append(commonArgs, "-vf", "scale=256:256", "-")
	a.Display, err = runFFmpegOnStream(displayArgs, content)
	if err != nil {
		return nil, fmt.Errorf("failed to create display avatar: %w", err)
	}

	thumbArgs := append(commonArgs, "-vf", "scale=96:96", "-")
	a.Thumb, err = runFFmpegOnStream(thumbArgs, bytes.NewReader(a.Display))
	if err != nil {
		return nil, fmt.Errorf("failed to create thumb avatar: %w", err)
	}

	return &a, nil
}
