package apiutil

import (
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"enterprise-go-rag/internal/contracts"
)

var NoisySequenceRegexp = regexp.MustCompile(`[A-Za-z0-9+/=]{36,}|[{}<>|~^]{2,}`)
var CGPAValueRegexp = regexp.MustCompile(`\b\d{1,2}(?:\.\d{1,2})?\s*/\s*10\b|\bcgpa\s*[:=-]?\s*\d{1,2}(?:\.\d{1,2})?\b`)
var SpacedGlyphRunRegexp = regexp.MustCompile(`(?:[A-Za-z0-9@._#+-]\s+){3,}[A-Za-z0-9@._#+-]`)

func NormalizeExtractedText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\uFFFD", " ")
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	trimmed = SpacedGlyphRunRegexp.ReplaceAllStringFunc(trimmed, compactSpacedGlyphRun)
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	return trimmed
}

func compactSpacedGlyphRun(raw string) string {
	tokens := strings.Fields(raw)
	if len(tokens) < 4 {
		return raw
	}
	var b strings.Builder
	for _, tok := range tokens {
		for _, r := range tok {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '@' || r == '.' || r == '_' || r == '+' || r == '-' || r == '#' {
				b.WriteRune(r)
			}
		}
	}
	joined := b.String()
	if len(joined) < 4 {
		return raw
	}
	return joined
}

func IsLikelyNoisyText(text string) bool {
	if text == "" {
		return true
	}
	if len(text) >= 64 {
		if NoisySequenceRegexp.MatchString(text) {
			return true
		}
	}
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return true
	}
	alpha, alnum, total, suspicious, symbol := 0, 0, 0, 0, 0
	for _, token := range tokens {
		tokenAlpha, tokenAlnum := 0, 0
		for _, r := range token {
			total++
			switch {
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
				alpha++
				tokenAlpha++
				alnum++
				tokenAlnum++
			case r >= '0' && r <= '9':
				alnum++
				tokenAlnum++
			case !(r == '_' || r == '-' || r == '.' || r == ',' || r == ':' || r == ';' || r == '?' || r == '!' || r == '/' || r == '(' || r == ')' || r == '[' || r == ']' || r == '\'' || r == '"'):
				symbol++
			}
		}
		if len(token) >= 24 {
			ratio := float64(tokenAlpha) / float64(MaxInt(tokenAlnum, 1))
			if ratio < 0.35 {
				suspicious++
			}
		}
	}
	if total == 0 {
		return true
	}
	alphaRatio := float64(alpha) / float64(total)
	alnumRatio := float64(alnum) / float64(total)
	suspiciousRatio := float64(suspicious) / float64(len(tokens))
	symbolRatio := float64(symbol) / float64(total)
	return alphaRatio < 0.28 || alnumRatio < 0.45 || suspiciousRatio > 0.25 || symbolRatio > 0.18
}

func MaxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func RankDocumentsByQueryIntent(query string, docs []contracts.RetrievalDocument) []contracts.RetrievalDocument {
	if len(docs) <= 1 {
		return docs
	}
	queryTerms := TokenizeSearchTerms(query)
	if len(queryTerms) == 0 {
		ordered := make([]contracts.RetrievalDocument, len(docs))
		copy(ordered, docs)
		return ordered
	}

	ordered := make([]contracts.RetrievalDocument, len(docs))
	copy(ordered, docs)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := queryIntentScore(queryTerms, ordered[i])
		right := queryIntentScore(queryTerms, ordered[j])
		if left == right {
			return ordered[i].Score > ordered[j].Score
		}
		return left > right
	})
	return ordered
}

func queryIntentScore(queryTerms []string, doc contracts.RetrievalDocument) int {
	content := strings.ToLower(doc.Content)
	source := strings.ToLower(doc.SourceURI)
	var metadata strings.Builder
	for key, value := range doc.Metadata {
		metadata.WriteString(" ")
		metadata.WriteString(strings.ToLower(key))
		metadata.WriteString(" ")
		metadata.WriteString(strings.ToLower(value))
	}

	metadataStr := metadata.String()
	score := 0
	for _, term := range queryTerms {
		switch {
		case strings.Contains(content, term):
			score += 8
		case strings.Contains(metadataStr, term):
			score += 5
		case strings.Contains(source, term):
			score += 4
		}
	}

	if ContainsAny(queryTerms, "cgpa", "gpa") {
		if strings.Contains(content, "cgpa") || strings.Contains(content, "gpa") {
			score += 20
		}
		if CGPAValueRegexp.MatchString(content) {
			score += 12
		}
	}
	if ContainsAny(queryTerms, "education", "qualification", "degree") {
		if strings.Contains(content, "education") || strings.Contains(content, "qualification") || strings.Contains(content, "degree") {
			score += 10
		}
	}
	return score
}

func ContainsAny(terms []string, values ...string) bool {
	if len(terms) == 0 || len(values) == 0 {
		return false
	}
	set := make(map[string]struct{})
	for _, term := range terms {
		set[term] = struct{}{}
	}
	for _, v := range values {
		if _, ok := set[v]; ok {
			return true
		}
	}
	return false
}

func TokenizeSearchTerms(input string) []string {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return nil
	}
	replacer := strings.NewReplacer("?", " ", "!", " ", ".", " ", ",", " ", ":", " ", ";", " ", "(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", "'", " ", "\"", " ")
	parts := strings.Fields(replacer.Replace(value))
	stop := map[string]struct{}{
		"what": {}, "is": {}, "the": {}, "of": {}, "for": {}, "and": {}, "with": {}, "from": {}, "about": {}, "tell": {}, "me": {},
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if _, skip := stop[p]; skip {
			continue
		}
		if len(p) < 3 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func HasReadableSignal(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	letters, total := 0, 0
	for _, r := range text {
		total++
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters++
		}
	}
	if total == 0 || letters == 0 {
		return false
	}
	return float64(letters)/float64(total) >= 0.15
}

func SanitizeAnswerChunk(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if IsLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 320 {
		return text[:320]
	}
	return text
}

func SanitizeStreamAnswer(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if IsLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 1200 {
		return text[:1200]
	}
	return text
}

func SanitizeContextChunk(raw string) string {
	text := NormalizeExtractedText(strings.TrimSpace(raw))
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 1800 {
		text = text[:1800]
	}
	if !HasReadableSignal(text) {
		return ""
	}
	if NoisySequenceRegexp.MatchString(text) && len(strings.Fields(text)) <= 3 {
		return ""
	}
	return text
}

func PrimaryQuerySignal(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(q, "cgpa"):
		return "cgpa"
	case strings.Contains(q, "gpa"):
		return "gpa"
	case strings.Contains(q, "education"):
		return "education"
	case strings.Contains(q, "qualification"):
		return "qualification"
	}
	for _, token := range strings.Fields(q) {
		if len(token) >= 4 {
			return token
		}
	}
	return ""
}

func BuildAnswerSnippet(query string, raw string) string {
	text := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	if text == "" {
		return ""
	}
	needle := PrimaryQuerySignal(query)
	if needle != "" {
		lower := strings.ToLower(text)
		if idx := strings.Index(lower, needle); idx >= 0 {
			start := idx - 120
			if start < 0 {
				start = 0
			}
			end := idx + len(needle) + 160
			if end > len(text) {
				end = len(text)
			}
			window := strings.TrimSpace(text[start:end])
			if sanitized := SanitizeAnswerChunk(window); sanitized != "" {
				return sanitized
			}
		}
	}
	return SanitizeAnswerChunk(text)
}

func BuildContextPart(query string, raw string) string {
	if cleaned := BuildAnswerSnippet(query, raw); cleaned != "" {
		return cleaned
	}
	return SanitizeContextChunk(raw)
}

func ChunkText(text string, maxLen int, overlapWords int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if maxLen <= 0 {
		maxLen = 900
	}
	if overlapWords < 0 {
		overlapWords = 0
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 {
		return nil
	}
	chunks := make([]string, 0, len(words)/20+1)
	start := 0
	for start < len(words) {
		end := start
		currentLen := 0
		for end < len(words) {
			wordLen := len(words[end])
			candidateLen := currentLen + wordLen
			if end > start {
				candidateLen++
			}
			if candidateLen > maxLen && end > start {
				break
			}
			currentLen = candidateLen
			end++
			if currentLen >= maxLen {
				break
			}
		}
		if end <= start {
			end = start + 1
		}
		chunks = append(chunks, strings.Join(words[start:end], " "))
		if end >= len(words) {
			break
		}
		nextStart := end - overlapWords
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}
	return chunks
}

func DecodePDFHexLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed)%2 == 1 {
		trimmed += "0"
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	if utf16Decoded, ok := decodeLikelyPDFUTF16(decoded); ok {
		return strings.TrimSpace(utf16Decoded)
	}
	b := make([]byte, 0, len(decoded))
	for _, ch := range decoded {
		if ch == '\n' || ch == '\r' || ch == '\t' || (ch >= 32 && ch <= 126) {
			b = append(b, ch)
		} else {
			b = append(b, ' ')
		}
	}
	return strings.TrimSpace(string(b))
}

func decodeLikelyPDFUTF16(decoded []byte) (string, bool) {
	if len(decoded) < 4 {
		return "", false
	}
	if len(decoded)%2 == 1 {
		decoded = decoded[:len(decoded)-1]
	}
	if len(decoded) < 4 {
		return "", false
	}
	evenZero, oddZero := 0, 0
	pairs := len(decoded) / 2
	for i := 0; i+1 < len(decoded); i += 2 {
		if decoded[i] == 0 {
			evenZero++
		}
		if decoded[i+1] == 0 {
			oddZero++
		}
	}
	if evenZero*3 < pairs && oddZero*3 < pairs {
		return "", false
	}
	words := make([]uint16, 0, pairs)
	if evenZero >= oddZero {
		for i := 0; i+1 < len(decoded); i += 2 {
			words = append(words, uint16(decoded[i])<<8|uint16(decoded[i+1]))
		}
	} else {
		for i := 0; i+1 < len(decoded); i += 2 {
			words = append(words, uint16(decoded[i+1])<<8|uint16(decoded[i]))
		}
	}
	runes := utf16.Decode(words)
	var clean strings.Builder
	for _, r := range runes {
		switch {
		case r == '\u0000':
			clean.WriteByte(' ')
		case r == '\n' || r == '\r' || r == '\t' || (r >= 32 && r <= 126):
			clean.WriteRune(r)
		default:
			clean.WriteByte(' ')
		}
	}
	text := strings.Join(strings.Fields(clean.String()), " ")
	if len(text) < 3 || IsLikelyNoisyText(text) {
		return "", false
	}
	return text, true
}
