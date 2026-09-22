package qrlabels

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/boombuler/barcode/qr"
	"image/color"
	"strings"
)

var ErrEmptyPayload = errors.New("qrlabels: payload must not be empty")

const errorCorrectionLevel = qr.Q

const quietZoneModules = 4

func ItemURL(origin, itemID string) string {
	origin = strings.TrimRight(origin, "/")
	return origin + "/i/" + itemID
}

func SVG(payload string) ([]byte, error) {
	if payload == "" {
		return nil, ErrEmptyPayload
	}

	code, err := qr.Encode(payload, errorCorrectionLevel, qr.Unicode)
	if err != nil {
		return nil, fmt.Errorf("qrlabels: encode QR for payload of length %d: %w", len(payload), err)
	}

	bounds := code.Bounds()
	dim := bounds.Dx()
	moduleSize := dim + 2*quietZoneModules

	var path strings.Builder
	for y := 0; y < dim; y++ {
		for x := 0; x < dim; x++ {
			if !isDarkModule(code.At(x, y)) {
				continue
			}
			fmt.Fprintf(&path, "M%d,%dh1v1h-1z", x+quietZoneModules, y+quietZoneModules)
		}
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&buf, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img">`, moduleSize, moduleSize)
	buf.WriteString("<title>")
	if err := xml.EscapeText(&buf, []byte(payload)); err != nil {
		return nil, fmt.Errorf("qrlabels: escape payload for SVG title: %w", err)
	}
	buf.WriteString("</title>")
	fmt.Fprintf(&buf, `<rect width="%d" height="%d" fill="#ffffff"/>`, moduleSize, moduleSize)
	if path.Len() > 0 {
		buf.WriteString(`<path fill="#000000" d="`)
		buf.WriteString(path.String())
		buf.WriteString(`"/>`)
	}
	buf.WriteString("</svg>")

	return buf.Bytes(), nil
}

func isDarkModule(c color.Color) bool {
	gray := color.GrayModel.Convert(c).(color.Gray)
	return gray.Y < 128
}
