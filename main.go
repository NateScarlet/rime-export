package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	marisa "github.com/pgaskin/go-marisa"
)

// Binary format structures (little-endian, matching librime's table.h)
// See: C:\Workspaces\librime\src\rime\dict\table.h
//      C:\Workspaces\librime\src\rime\dict\mapped_file.h

// Entry represents a dictionary entry with text, code, and weight.
type Entry struct {
	Text   string
	Code   string // resolved syllable strings joined by '/'
	Weight float32
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <table.bin> [output.txt]\n", os.Args[0])
		os.Exit(1)
	}

	inputPath := os.Args[1]
	outputPath := ""
	if len(os.Args) >= 3 {
		outputPath = os.Args[2]
	}

	if err := exportTable(inputPath, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func exportTable(inputPath, outputPath string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// 1. Parse Metadata header (68 bytes at offset 0)
	if len(data) < 68 {
		return errors.New("file too small for metadata")
	}

	format := strings.TrimRight(string(data[0:32]), "\x00")
	fmt.Fprintf(os.Stderr, "Format: %s\n", format)

	dictChecksum := binary.LittleEndian.Uint32(data[32:36])
	numSyllables := binary.LittleEndian.Uint32(data[36:40])
	numEntries := binary.LittleEndian.Uint32(data[40:44])

	// OffsetPtr: offset is relative to the pointer field itself
	// The syllabary OffsetPtr is at byte 44 (0x2C), value at 44-47
	syllOffset := int32(binary.LittleEndian.Uint32(data[44:48]))
	syllAddr := int64(44) + int64(syllOffset)

	// Index OffsetPtr is at byte 48 (0x30)
	indexOffset := int32(binary.LittleEndian.Uint32(data[48:52]))
	indexAddr := int64(48) + int64(indexOffset)

	// reserved_1 at 52, reserved_2 at 56
	// String table OffsetPtr at byte 60 (0x3C)
	strtabOffset := int32(binary.LittleEndian.Uint32(data[60:64]))
	strtabAddr := int64(60) + int64(strtabOffset)

	strtabSize := binary.LittleEndian.Uint32(data[64:68])

	fmt.Fprintf(os.Stderr, "Dict checksum: %#x\n", dictChecksum)
	fmt.Fprintf(os.Stderr, "Num syllables: %d\n", numSyllables)
	fmt.Fprintf(os.Stderr, "Num entries: %d\n", numEntries)
	fmt.Fprintf(os.Stderr, "Syllabary at: %#x\n", syllAddr)
	fmt.Fprintf(os.Stderr, "Index at: %#x\n", indexAddr)
	fmt.Fprintf(os.Stderr, "String table at: %#x, size: %d\n", strtabAddr, strtabSize)

	// 2. Load string table (marisa trie)
	if strtabAddr < 0 || int64(strtabAddr) >= int64(len(data)) {
		return errors.New("string table offset out of range")
	}
	if int64(strtabAddr)+int64(strtabSize) > int64(len(data)) {
		return errors.New("string table extends beyond file")
	}
	trieBytes := data[strtabAddr : int64(strtabAddr)+int64(strtabSize)]

	trie, err := marisa.New(trieBytes)
	if err != nil {
		return fmt.Errorf("load marisa trie: %w", err)
	}

	// Helper to resolve a string ID
	resolveText := func(id int32) string {
		s, ok, _ := trie.ReverseLookup(uint32(id))
		if !ok {
			return fmt.Sprintf("?%d", id)
		}
		return s
	}

	// 3. Read syllabary (Array<StringType> where StringType = int32 StringId)
	if syllAddr < 0 || int64(syllAddr)+4 > int64(len(data)) {
		return errors.New("syllabary offset out of range")
	}
	sylSize := binary.LittleEndian.Uint32(data[syllAddr : syllAddr+4])
	if int64(syllAddr)+4+int64(sylSize)*4 > int64(len(data)) {
		return errors.New("syllabary extends beyond file")
	}
	sylIDs := make([]int32, sylSize)
	for i := uint32(0); i < sylSize; i++ {
		off := syllAddr + 4 + int64(i)*4
		sylIDs[i] = int32(binary.LittleEndian.Uint32(data[off : off+4]))
	}

	resolveSyl := func(sid int32) string {
		if sid < 0 || sid >= int32(len(sylIDs)) {
			return fmt.Sprintf("?%d", sid)
		}
		return resolveText(sylIDs[sid])
	}

	// 4. Traverse index tree
	var allEntries []Entry

	buf := &buffer{data: data}

	// Read from index position
	buf.pos = indexAddr

	headSize := buf.readU32()
	for sylID := uint32(0); sylID < headSize; sylID++ {
		entries := buf.readEntryList(resolveText)
		nextOff := buf.readI32()

		sylStr := resolveSyl(int32(sylID))
		for _, e := range entries {
			allEntries = append(allEntries, Entry{
				Text:   e.text,
				Code:   sylStr,
				Weight: e.weight,
			})
		}

		if nextOff != 0 {
			// next_level OffsetPtr is 4 bytes behind current position
			nextAddr := buf.pos - 4 + int64(nextOff)
			traverseTrunk(buf, nextAddr, []int32{int32(sylID)}, 0,
				resolveText, resolveSyl, &allEntries)
		}
	}

	fmt.Fprintf(os.Stderr, "Traversed %d entries\n", len(allEntries))

	// 5. Sort by weight (descending)
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].Weight > allEntries[j].Weight
	})

	// 6. Convert 双拼→全拼 and output
	var out io.Writer = os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		defer f.Close()
		out = f
	}

	for _, e := range allEntries {
		quanpin := convertToQuanpin(e.Code)
		if quanpin == "" {
			continue
		}
		// 格式3：拼音 pin|yin 123
		fmt.Fprintf(out, "%s %s %.0f\n", e.Text, quanpin, e.Weight)
	}

	return nil
}

type entryPair struct {
	text   string
	weight float32
}

// buffer wraps binary data with a read position.
type buffer struct {
	data []byte
	pos  int64
}

func (b *buffer) readU32() uint32 {
	v := binary.LittleEndian.Uint32(b.data[b.pos : b.pos+4])
	b.pos += 4
	return v
}

func (b *buffer) readI32() int32 {
	v := binary.LittleEndian.Uint32(b.data[b.pos : b.pos+4])
	b.pos += 4
	return int32(v)
}

func (b *buffer) readF32() float32 {
	v := binary.LittleEndian.Uint32(b.data[b.pos : b.pos+4])
	b.pos += 4
	return float32FromBits(v)
}

func float32FromBits(b uint32) float32 {
	return math.Float32frombits(b)
}

// readEntryList reads List<Entry> = { uint32 size, OffsetPtr<Entry> at (int32) }
func (b *buffer) readEntryList(resolveText func(int32) string) []entryPair {
	size := b.readU32()
	atPos := b.pos
	entryOff := b.readI32()

	if size == 0 || entryOff == 0 {
		return nil
	}

	entryAddr := atPos + int64(entryOff)
	saved := b.pos
	defer func() { b.pos = saved }()

	b.pos = entryAddr
	result := make([]entryPair, 0, size)
	for i := uint32(0); i < size; i++ {
		textID := b.readI32()
		weight := b.readF32()
		result = append(result, entryPair{
			text:   resolveText(textID),
			weight: weight,
		})
	}
	return result
}

// traverseTrunk traverses TrunkIndex or TailIndex recursively.
func traverseTrunk(
	b *buffer,
	addr int64,
	prefix []int32,
	depth int,
	resolveText func(int32) string,
	resolveSyl func(int32) string,
	entries *[]Entry,
) {
	saved := b.pos
	defer func() { b.pos = saved }()

	b.pos = addr
	arrSize := b.readU32()

	if depth < 2 {
		// TrunkIndex: Array<TrunkIndexNode>
		for i := uint32(0); i < arrSize; i++ {
			key := b.readI32()
			nodeEntries := b.readEntryList(resolveText)
			nextOff := b.readI32()

			code := append(append([]int32{}, prefix...), key)
			codeStr := codeToString(code, resolveSyl)
			for _, e := range nodeEntries {
				*entries = append(*entries, Entry{
					Text:   e.text,
					Code:   codeStr,
					Weight: e.weight,
				})
			}

			if nextOff != 0 {
				nextAddr := b.pos - 4 + int64(nextOff)
				traverseTrunk(b, nextAddr, code, depth+1,
					resolveText, resolveSyl, entries)
			}
		}
	} else {
		// TailIndex: Array<LongEntry>
		for i := uint32(0); i < arrSize; i++ {
			extraSize := b.readU32()
			extraAt := b.pos
			extraOff := b.readI32()

			textID := b.readI32()
			weight := b.readF32()

			var extra []int32
			if extraSize > 0 && extraOff != 0 {
				saved2 := b.pos
				b.pos = extraAt + int64(extraOff)
				extra = make([]int32, extraSize)
				for j := uint32(0); j < extraSize; j++ {
					extra[j] = b.readI32()
				}
				b.pos = saved2
			}

			code := append(append([]int32{}, prefix...), extra...)
			codeStr := codeToString(code, resolveSyl)
			*entries = append(*entries, Entry{
				Text:   resolveText(textID),
				Code:   codeStr,
				Weight: weight,
			})
		}
	}
}

func codeToString(code []int32, resolveSyl func(int32) string) string {
	parts := make([]string, len(code))
	for i, sid := range code {
		parts[i] = resolveSyl(sid)
	}
	return strings.Join(parts, "/")
}

// 双拼→全拼 conversion based on 小鹤双拼 layout
// The codes in the table are in internal (post-algebra) format.
// The algebra in main.schema.yaml maps 搜狗→小鹤, so internal codes use 小鹤 layout.
//
// Each syllable in the syllabary has format like: "dj[kd" or "aa[bk"
// where the part before '[' is the 双拼, and after is 辅助码.
// For multi-char words, codes are joined with '/'.

// 搜狗双拼 韵母映射 (this is what the table stores)
// The speller algebra converts 搜狗→小鹤, so table codes are in 搜狗 format
var finalMap = map[byte]string{
	'a': "a",
	'b': "in",
	'c': "ao",
	'd': "ai",
	'e': "e",
	'f': "en",
	'g': "eng",
	'h': "ang",
	'i': "i",
	'j': "an",
	'k': "ing/uai",
	'l': "iang/uang",
	'm': "ian",
	'n': "iao",
	'o': "o/uo",
	'p': "ie",
	'q': "iu",
	'r': "uan/er",
	's': "ong/iong",
	't': "ue/ve",
	'u': "u",
	'v': "ui",
	'w': "ei",
	'x': "ia/ua",
	'y': "un",
	'z': "ou",
	';': "ing",
}

// 小鹤双拼 声母 → 全拼 Initial mapping
var initialMap = map[byte]string{
	'b': "b",
	'c': "c",
	'd': "d",
	'f': "f",
	'g': "g",
	'h': "h",
	'j': "j",
	'k': "k",
	'l': "l",
	'm': "m",
	'n': "n",
	'p': "p",
	'q': "q",
	'r': "r",
	's': "s",
	't': "t",
	'w': "w",
	'x': "x",
	'y': "y",
	'z': "z",
	'v': "zh",
	'i': "ch",
	'u': "sh",
}

// convertToQuanpin converts a code string like "dj[kd/hg[xd" to full pinyin "dan|heng"
func convertToQuanpin(code string) string {
	parts := strings.Split(code, "/")
	var result []string
	for _, part := range parts {
		// Extract the 双拼 part (before '[')
		sp := part
		if idx := strings.IndexByte(part, '['); idx >= 0 {
			sp = part[:idx]
		}
		// Also handle other delimiters that might appear
		sp = strings.TrimRight(sp, "0123456789")

		qp := spToQuanpin(sp)
		if qp != "" {
			result = append(result, qp)
		}
	}
	return strings.Join(result, "|")
}

// spToQuanpin converts a single 双拼 code (e.g., "dj") to 全拼 (e.g., "dan")
func spToQuanpin(sp string) string {
	if len(sp) == 0 {
		return ""
	}

	// Single letter: zero-initial syllable
	if len(sp) == 1 {
		c := sp[0]
		if f, ok := finalMap[c]; ok {
			return resolveAmbiguous(f, '.', c)
		}
		return string(c)
	}

	// Two letters: initial + final
	first := sp[0]
	second := sp[1]

	// Check if second char is a valid final
	final, hasFinal := finalMap[second]
	if !hasFinal {
		return sp // can't decode
	}

	// Check if first char is a valid initial
	initialStr, hasInitial := initialMap[first]
	if hasInitial {
		fin := resolveAmbiguous(final, first, second)
		return initialStr + fin
	}

	// Check zero-initial: first is a final, second is also a "final"
	// This handles cases like "aa" = a (啊), "ee" = e (额), "oo" = o (哦)
	if _, ok1 := finalMap[first]; ok1 {
		// Zero-initial: the first char is the 韵母
		fin := resolveAmbiguous(finalMap[first], '.', first)
		return fin
	}

	return sp
}

// resolveAmbiguous picks the most likely final from an ambiguous set.
// For now, just returns the first option (simplification).
func resolveAmbiguous(final string, initial, key byte) string {
	if !strings.Contains(final, "/") {
		return final
	}
	// For ambiguous cases, use context to disambiguate
	// uan/er: if zero-initial, likely "er"; with initial, "uan"
	// iang/uang: depends on initial
	// ong/iong: depends on initial
	// ue/ve: depends on initial
	// ia/ua: depends on initial
	// o/uo: depends on initial

	options := strings.Split(final, "/")
	switch {
	case final == "uan/er":
		if initial == '.' {
			return "er"
		}
		return "uan"
	case final == "iang/uang":
		if initial == 'j' || initial == 'q' || initial == 'x' || initial == 'l' || initial == 'n' {
			return "iang"
		}
		return "uang"
	case final == "ong/iong":
		if initial == 'j' || initial == 'q' || initial == 'x' {
			return "iong"
		}
		return "ong"
	case final == "ue/ve":
		if initial == 'j' || initial == 'q' || initial == 'x' || initial == 'y' {
			return "ue"
		}
		if initial == 'n' || initial == 'l' {
			return "ve"
		}
		return "ue"
	case final == "ia/ua":
		if initial == 'j' || initial == 'q' || initial == 'x' || initial == 'l' ||
			initial == 'd' || initial == 't' || initial == 'n' {
			return "ia"
		}
		return "ua"
	case final == "o/uo":
		if initial == 'b' || initial == 'p' || initial == 'm' || initial == 'f' ||
			initial == 'y' || initial == 'w' {
			return "o"
		}
		return "uo"
	}
	return options[0]
}
