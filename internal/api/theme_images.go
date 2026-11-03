package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/flagfall/flagfall/internal/blobs"
	"github.com/flagfall/flagfall/internal/store"
	"github.com/flagfall/flagfall/internal/themes"
)

// Theme images: files organizers upload for a theme's image settings (logo,
// favicon). A setting stores the image's URL, /theme-assets/{sha256}.

const themeAssetPrefix = "/theme-assets/"

var themeAssetRE = regexp.MustCompile(`^/theme-assets/([0-9a-f]{64})$`)

// adminUploadThemeImage stores an image for one image setting and returns
// its URL. The draft isn't changed: the admin form puts the URL in the
// setting and saves the draft as usual.
func (a *API) adminUploadThemeImage(w http.ResponseWriter, r *http.Request) error {
	t, err := a.installedTheme(r)
	if err != nil {
		return err
	}
	key := chi.URLParam(r, "key")
	var setting *themes.Setting
	for i := range t.Manifest.Settings {
		if s := &t.Manifest.Settings[i]; s.Key == key && s.Type == "image" {
			setting = s
		}
	}
	if setting == nil {
		return errNotFound
	}
	max := setting.MaxBytes
	if max <= 0 {
		max = themes.MaxImageBytes
	}
	accept := setting.Accept
	if len(accept) == 0 {
		accept = themes.ImageTypes
	}

	r.Body = http.MaxBytesReader(w, r.Body, max+(64<<10))
	mr, err := r.MultipartReader()
	if err != nil {
		return errBadRequest("Upload the image as multipart/form-data in a field named \"file\".")
	}
	var data []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return errBadRequest("No \"file\" field in the upload.")
		}
		if err != nil {
			return errValidation(map[string]string{"file": "The image is too large: " + sizeLabel(max) + " at most."})
		}
		if part.FormName() != "file" {
			continue
		}
		data, err = io.ReadAll(io.LimitReader(part, max+1))
		if err != nil {
			return errBadRequest("The upload was cut off.")
		}
		break
	}
	if int64(len(data)) > max {
		return errValidation(map[string]string{"file": "The image is too large: " + sizeLabel(max) + " at most."})
	}
	ctype := sniffImage(data)
	if ctype == "" || !contains(accept, ctype) {
		return errValidation(map[string]string{"file": "Use one of these formats: " + formatList(accept) + "."})
	}

	ctx := r.Context()
	sum, err := a.files.PutBytes(ctx, data)
	if err != nil {
		return err
	}
	row := &store.ThemeImage{SHA256: sum, ContentType: ctype, Size: int64(len(data)), CreatedAt: a.now().UTC()}
	if u := currentUser(r); u != nil {
		row.UploadedBy = &u.ID
	}
	if _, err := a.store.DB.NewInsert().Model(row).On("CONFLICT (sha256) DO NOTHING").Exec(ctx); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": themeAssetPrefix + sum, "contentType": ctype, "size": len(data)})
	return nil
}

// themeImageCheck returns a function reporting whether a URL names an
// uploaded theme image. It loads the (small) list up front, so it is safe
// to call inside a transaction: SQLite runs with a single connection.
func (a *API) themeImageCheck(ctx context.Context) (func(string) bool, error) {
	var sums []string
	if err := a.store.DB.NewSelect().Model((*store.ThemeImage)(nil)).Column("sha256").Scan(ctx, &sums); err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(sums))
	for _, s := range sums {
		known[s] = true
	}
	return func(url string) bool {
		m := themeAssetRE.FindStringSubmatch(url)
		return m != nil && known[m[1]]
	}, nil
}

// serveThemeAsset serves an uploaded theme image. Only blobs recorded in
// theme_images are served, so a challenge file can't be fetched here by
// its hash.
func (a *API) serveThemeAsset(w http.ResponseWriter, r *http.Request) {
	sum := chi.URLParam(r, "sha256")
	if !blobs.ValidKey(sum) {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	img := new(store.ThemeImage)
	if err := a.store.DB.NewSelect().Model(img).Where("sha256 = ?", sum).Scan(ctx); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			a.log.Error("theme image lookup", "err", err)
		}
		http.NotFound(w, r)
		return
	}
	f, err := a.files.Open(ctx, sum)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", img.ContentType)
	h.Set("Content-Length", strconv.FormatInt(img.Size, 10))
	h.Set("X-Content-Type-Options", "nosniff")
	// Content-addressed, so it never changes.
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	// An SVG opened directly is a document: keep any script in it from
	// running on this origin.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = io.Copy(w, f)
}

// sniffImage returns the image's type if it is one of themes.ImageTypes.
func sniffImage(data []byte) string {
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg", "image/webp":
		return ct
	}
	if isSVG(data) {
		return "image/svg+xml"
	}
	return ""
}

// isSVG reports whether data is an XML document whose root is <svg>.
func isSVG(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch el := tok.(type) {
		case xml.StartElement:
			return strings.EqualFold(el.Name.Local, "svg")
		case xml.CharData:
			if len(bytes.TrimSpace(el)) > 0 {
				return false
			}
		}
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func formatList(types []string) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = map[string]string{"image/png": "PNG", "image/jpeg": "JPEG", "image/webp": "WebP", "image/svg+xml": "SVG"}[t]
	}
	return strings.Join(names, ", ")
}

func sizeLabel(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%g MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}
