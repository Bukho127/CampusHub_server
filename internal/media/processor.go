package media

import (
	"context"
	"io"
	"os/exec"
	"time"
)

// Processor writes a same-format image to a server-controlled output path.
type Processor interface {
	Optimize(context.Context, string, string, string) error
}

type FFmpeg struct {
	binary string
}

func NewFFmpeg(binary string) (*FFmpeg, error) {
	if binary == "" {
		binary = "ffmpeg"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &FFmpeg{binary: resolved}, nil
}

// Optimize preserves resolution. PNG encoding is lossless; JPEG uses a fixed
// high quality setting rather than progressively degrading to meet a byte cap.
func (p *FFmpeg) Optimize(ctx context.Context, input, output, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-y",
		"-max_alloc", "134217728",
		"-protocol_whitelist", "file,pipe",
		"-threads", "1",
		"-i", input,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-an",
		"-sn",
		"-dn",
		"-map_metadata", "-1",
		"-threads", "1",
	}

	switch contentType {
	case "image/jpeg":
		args = append(args,
			"-c:v", "mjpeg",
			"-q:v", "2",
			"-pix_fmt", "yuvj444p",
		)
	case "image/png":
		args = append(args,
			"-c:v", "png",
			"-compression_level", "9",
		)
	default:
		return ErrUnsupportedType
	}

	args = append(args,
		"-fs", "20971521",
		"-f", "image2",
		output,
	)

	// Never invoke a shell or use a client-supplied path, URL, or FFmpeg option.
	command := exec.CommandContext(ctx, p.binary, args...)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.WaitDelay = 2 * time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrProcessing
	}
	return nil
}
