package appapi

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"orbit/internal/inbox"
)

func (s *Server) upload(w http.ResponseWriter, r *http.Request, node string) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		failure(w, &inbox.Fault{Code: "image_too_large"})
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 20000000 {
		failure(w, &inbox.Fault{Code: "invalid_image"})
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		failure(w, &inbox.Fault{Code: "invalid_image"})
		return
	}
	id := uuid.NewString()
	if err = os.MkdirAll(s.files, 0700); err != nil {
		failure(w, err)
		return
	}
	original := filepath.Join(s.files, id)
	thumbnail := original + ".jpg"
	if err = os.WriteFile(original, data, 0600); err != nil {
		failure(w, err)
		return
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(original)
			_ = os.Remove(thumbnail)
		}
	}()
	width, height := cfg.Width, cfg.Height
	if width > 480 || height > 480 {
		if width >= height {
			height = max(1, height*480/width)
			width = 480
		} else {
			width = max(1, width*480/height)
			height = 480
		}
	}
	small := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			small.Set(x, y, img.At(img.Bounds().Min.X+x*cfg.Width/width, img.Bounds().Min.Y+y*cfg.Height/height))
		}
	}
	var thumb bytes.Buffer
	if err = jpeg.Encode(&thumb, small, &jpeg.Options{Quality: 85}); err != nil {
		failure(w, err)
		return
	}
	if err = os.WriteFile(thumbnail, thumb.Bytes(), 0600); err != nil {
		failure(w, err)
		return
	}
	a := inbox.Attachment{ID: id, MIME: "image/" + format, Size: int64(len(data)), Width: cfg.Width, Height: cfg.Height}
	if err = s.store.AddAttachment(r.Context(), node, a); err != nil {
		failure(w, err)
		return
	}
	success = true
	write(w, 201, a)
}
func (s *Server) download(w http.ResponseWriter, r *http.Request, node string) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
		failure(w, &inbox.Fault{Code: "attachment_unavailable"})
		return
	}
	a, err := s.store.Attachment(r.Context(), node, id)
	if err != nil {
		failure(w, err)
		return
	}
	path := filepath.Join(s.files, id)
	if r.URL.Query().Get("thumbnail") == "1" {
		path += ".jpg"
		a.MIME = "image/jpeg"
	}
	f, err := os.Open(path)
	if err != nil {
		failure(w, &inbox.Fault{Code: "attachment_unavailable"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Content-Type", a.MIME)
	http.ServeContent(w, r, id, info.ModTime(), f)
}
func (s *Server) cleanup(ctx context.Context) {
	ids, err := s.store.ExpiredAttachments(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		_ = os.Remove(filepath.Join(s.files, id))
		_ = os.Remove(filepath.Join(s.files, id+".jpg"))
	}
	// Reclaim files left by a process crash before metadata was committed.
	entries, _ := os.ReadDir(s.files)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		id := entry.Name()
		if filepath.Ext(id) == ".jpg" {
			id = id[:len(id)-4]
		}
		// Owner-independent existence is checked through an explicit store method.
		if !s.store.HasAttachment(ctx, id) {
			_ = os.Remove(filepath.Join(s.files, entry.Name()))
		}
	}
}
