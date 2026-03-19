package apiutil

import (
	"bytes"
	"compress/zlib"
	"io"
	"strings"

	pdf "github.com/ledongthuc/pdf"
	rscpdf "rsc.io/pdf"
)

func ExtractPDFSearchableText(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	if parsed := extractPDFTextWithLibrary(payload); parsed != "" {
		parsed = NormalizeExtractedText(parsed)
		if parsed == "" {
			return ""
		}
		if len(parsed) > 12000 {
			return parsed[:12000]
		}
		return parsed
	}
	if parsed := extractPDFTextWithRscLibrary(payload); parsed != "" {
		parsed = NormalizeExtractedText(parsed)
		if parsed == "" {
			return ""
		}
		if len(parsed) > 12000 {
			return parsed[:12000]
		}
		return parsed
	}
	segments := make([]string, 0, 128)
	segments = append(segments, extractPDFTextSegments(payload)...)
	for _, decoded := range extractPDFFlateStreams(payload) {
		segments = append(segments, extractPDFTextSegments(decoded)...)
	}

	if len(segments) == 0 {
		return ""
	}
	unique := make([]string, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	if len(unique) == 0 {
		return ""
	}
	text := NormalizeExtractedText(strings.Join(unique, " "))
	if text == "" {
		return ""
	}
	if IsLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 12000 {
		return text[:12000]
	}
	return text
}

func extractPDFTextWithRscLibrary(payload []byte) string {
	reader, err := rscpdf.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return ""
	}
	var b strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		content := page.Content()
		for _, textPart := range content.Text {
			segment := strings.TrimSpace(textPart.S)
			if segment == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(segment)
		}
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if len(text) < 10 || IsLikelyNoisyText(text) {
		return ""
	}
	return text
}

func extractPDFTextWithLibrary(payload []byte) string {
	reader, err := pdf.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return ""
	}
	plain, err := reader.GetPlainText()
	if err != nil || plain == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(plain, 2*1024*1024))
	if err != nil || len(raw) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	if len(text) < 10 {
		return ""
	}
	if IsLikelyNoisyText(text) {
		return ""
	}
	return text
}

func extractPDFTextSegments(payload []byte) []string {
	segments := make([]string, 0, 64)
	current := make([]rune, 0, 64)
	inLiteral, escape, inHex := false, false, false
	var hexCurrent []rune

	appendCandidate := func(candidate string) {
		candidate = strings.Join(strings.Fields(candidate), " ")
		if len(candidate) < 3 {
			return
		}
		if IsLikelyNoisyText(candidate) {
			return
		}
		segments = append(segments, candidate)
	}
	flush := func() {
		if len(current) == 0 {
			return
		}
		candidate := string(current)
		current = current[:0]
		appendCandidate(candidate)
	}
	flushHex := func() {
		if len(hexCurrent) == 0 {
			return
		}
		decoded := DecodePDFHexLiteral(string(hexCurrent))
		hexCurrent = hexCurrent[:0]
		if decoded == "" {
			return
		}
		appendCandidate(decoded)
	}

	for _, b := range payload {
		r := rune(b)
		if !inLiteral && !inHex {
			switch r {
			case '(':
				inLiteral, escape = true, false
				current = current[:0]
			case '<':
				inHex = true
				hexCurrent = hexCurrent[:0]
			}
			continue
		}
		if inHex {
			if r == '>' {
				flushHex()
				inHex = false
				continue
			}
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				hexCurrent = append(hexCurrent, r)
			}
			continue
		}
		if escape {
			escape = false
			if r >= 32 && r <= 126 {
				current = append(current, r)
			}
			continue
		}
		switch r {
		case '\\':
			escape = true
		case ')':
			flush()
			inLiteral = false
		default:
			if r == '\n' || r == '\r' || r == '\t' || (r >= 32 && r <= 126) {
				current = append(current, r)
			}
		}
	}
	flush()
	flushHex()
	return segments
}

func extractPDFFlateStreams(payload []byte) [][]byte {
	streams := make([][]byte, 0, 8)
	lower := toASCIILower(payload)
	start := 0
	for {
		if start >= len(lower) {
			break
		}
		streamIdx := bytes.Index(lower[start:], []byte("stream"))
		if streamIdx < 0 {
			break
		}
		streamIdx += start
		dataStart := streamIdx + len("stream")
		if dataStart < len(payload) {
			if payload[dataStart] == '\r' {
				dataStart++
			}
			if dataStart < len(payload) && payload[dataStart] == '\n' {
				dataStart++
			}
		}
		if dataStart >= len(lower) {
			break
		}
		endIdx := bytes.Index(lower[dataStart:], []byte("endstream"))
		if endIdx < 0 {
			break
		}
		endIdx += dataStart
		if endIdx <= dataStart {
			start = dataStart + 1
			continue
		}
		raw := payload[dataStart:endIdx]
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			decoded, readErr := io.ReadAll(io.LimitReader(zr, 2*1024*1024))
			_ = zr.Close()
			if readErr == nil && len(decoded) > 0 {
				streams = append(streams, decoded)
			}
		}
		start = endIdx + len("endstream")
	}
	return streams
}

func toASCIILower(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] = out[i] + ('a' - 'A')
		}
	}
	return out
}
