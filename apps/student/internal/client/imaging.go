package client

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
)

// The server keeps screenshots as JPEG of at most two 1080p screens' worth of pixels, the longer side at most 3840
// (internal/service/screenshots.go). Larger desktops are scaled down here, before the upload, which also keeps the
// classroom network free: a screenshot is taken every few seconds while a student is away.
const (
	maxUploadPixels   = 2 * 1920 * 1080
	maxUploadSide     = 3840
	uploadJPEGQuality = 85
)

// encodeCapture turns a captured PNG into the JPEG that is uploaded, scaled to the server's limits.
func encodeCapture(data []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	img = fitUpload(img)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: uploadJPEGQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// fitUpload scales an image down, keeping its shape, until it fits the server's limits; a smaller one is returned as is.
func fitUpload(img image.Image) image.Image {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	scale := math.Max(math.Sqrt(float64(w)*float64(h)/maxUploadPixels), float64(max(w, h))/maxUploadSide)
	if scale <= 1 {
		return img
	}
	tw, th := max(1, int(float64(w)/scale)), max(1, int(float64(h)/scale))
	for int64(tw)*int64(th) > maxUploadPixels || max(tw, th) > maxUploadSide {
		tw, th = max(1, tw-1), max(1, th-1)
	}
	return shrink(img, tw, th)
}

// shrink averages every source pixel into the target pixel it falls in: text stays legible where dropping pixels would
// lose thin strokes.
func shrink(img image.Image, tw, th int) *image.RGBA {
	source := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(source, source.Bounds(), img, img.Bounds().Min, draw.Src)
	w, h := source.Bounds().Dx(), source.Bounds().Dy()
	sums := make([][4]uint64, tw*th)
	counts := make([]uint64, tw*th)
	for y := 0; y < h; y++ {
		ty := y * th / h
		row := source.Pix[y*source.Stride:]
		for x := 0; x < w; x++ {
			at := ty*tw + x*tw/w
			pixel := row[x*4 : x*4+4]
			sums[at][0] += uint64(pixel[0])
			sums[at][1] += uint64(pixel[1])
			sums[at][2] += uint64(pixel[2])
			sums[at][3] += uint64(pixel[3])
			counts[at]++
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	for i, sum := range sums {
		if n := counts[i]; n > 0 {
			copy(out.Pix[i*4:i*4+4], []byte{byte(sum[0] / n), byte(sum[1] / n), byte(sum[2] / n), byte(sum[3] / n)})
		}
	}
	return out
}
