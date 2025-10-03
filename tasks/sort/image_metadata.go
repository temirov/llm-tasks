package sort

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/temirov/llm-tasks/internal/fsops"
)

func collectImageMetadata(info fsops.FileInfo) map[string]string {
	ext := strings.ToLower(info.Extension)
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".heic", ".heif", ".tiff", ".tif":
	default:
		return nil
	}

	file, err := os.Open(info.AbsolutePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil
	}

	metadata := make(map[string]string)
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		metadata["width"] = strconv.Itoa(cfg.Width)
		metadata["height"] = strconv.Itoa(cfg.Height)
		metadata["format"] = format
	}

	if strings.Contains(info.MIMEType, "jpeg") || ext == ".jpg" || ext == ".jpeg" {
		if exifData, err := exif.Decode(bytes.NewReader(data)); err == nil {
			populateExifFields(exifData, metadata)
		}
	}

	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func populateExifFields(x *exif.Exif, metadata map[string]string) {
	if tm, err := x.DateTime(); err == nil {
		metadata["datetime"] = tm.UTC().Format(time.RFC3339)
	}
	if model, err := x.Get(exif.Model); err == nil {
		metadata["camera_model"] = cleanExifString(model.String())
	}
	if make, err := x.Get(exif.Make); err == nil {
		metadata["camera_make"] = cleanExifString(make.String())
	}
	if lens, err := x.Get(exif.LensModel); err == nil {
		metadata["lens_model"] = cleanExifString(lens.String())
	}
	if exposure, err := x.Get(exif.ExposureTime); err == nil {
		metadata["exposure_time"] = cleanExifString(exposure.String())
	}
	if iso, err := x.Get(exif.ISOSpeedRatings); err == nil {
		metadata["iso"] = cleanExifString(iso.String())
	}
	if focal, err := x.Get(exif.FocalLength); err == nil {
		metadata["focal_length"] = cleanExifString(focal.String())
	}
	if lat, long, err := x.LatLong(); err == nil {
		metadata["gps_latitude"] = fmt.Sprintf("%.6f", lat)
		metadata["gps_longitude"] = fmt.Sprintf("%.6f", long)
	}
}

func cleanExifString(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimSuffix(trimmed, "\u0000")
	trimmed = strings.TrimSuffix(trimmed, "\000")
	return trimmed
}
