package main

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"

	marisa "github.com/pgaskin/go-marisa"
)

// Integration test: extract specific words from actual table.bin
func TestExtractKnownWords(t *testing.T) {
	data, err := os.ReadFile("input/main.table.bin")
	if err != nil {
		t.Skipf("table.bin not available: %v", err)
	}

	// Parse metadata
	format := strings.TrimRight(string(data[0:32]), "\x00")
	if format != "Rime::Table/4.0" {
		t.Fatalf("unexpected format: %s", format)
	}

	syllOffset := int64(44) + int64(int32(binary.LittleEndian.Uint32(data[44:48])))
	indexOffset := int64(48) + int64(int32(binary.LittleEndian.Uint32(data[48:52])))
	strtabAddr := int64(60) + int64(int32(binary.LittleEndian.Uint32(data[60:64])))
	strtabSize := binary.LittleEndian.Uint32(data[64:68])

	// Load string table
	trieBytes := data[strtabAddr : int64(strtabAddr)+int64(strtabSize)]
	trie, err := marisa.New(trieBytes)
	if err != nil {
		t.Fatalf("load marisa: %v", err)
	}

	resolveText := func(id int32) string {
		s, ok, _ := trie.ReverseLookup(uint32(id))
		if !ok {
			return ""
		}
		return s
	}

	// Read syllabary
	sylSize := binary.LittleEndian.Uint32(data[syllOffset : syllOffset+4])
	sylIDs := make([]int32, sylSize)
	for i := uint32(0); i < sylSize; i++ {
		off := syllOffset + 4 + int64(i)*4
		sylIDs[i] = int32(binary.LittleEndian.Uint32(data[off : off+4]))
	}

	resolveSyl := func(sid int32) string {
		if sid < 0 || sid >= int32(len(sylIDs)) {
			return ""
		}
		return resolveText(sylIDs[sid])
	}

	// Quick scan: check specific words
	wantWords := map[string]string{
		"丹恒":   "dan|heng",
		"七圣召唤": "qi|sheng|zhao|huan",
		"呜呜伯":  "wu|wu|bo",
		"能撑住":  "neng|cheng|zhu",
	}
	found := make(map[string]string)

	buf := &buffer{data: data}
	buf.pos = indexOffset
	headSize := buf.readU32()

	// Scan all syllables (but stop when all words found)
	limit := headSize
	if headSize < limit {
		limit = headSize
	}

	for sylID := uint32(0); sylID < limit; sylID++ {
		entries := buf.readEntryList(resolveText)
		nextOff := buf.readI32()
		sylStr := resolveSyl(int32(sylID))

		for _, e := range entries {
			if _, want := wantWords[e.text]; want {
				found[e.text] = sylStr
			}
		}

		if nextOff != 0 {
			nextAddr := buf.pos - 4 + int64(nextOff)
			scanTrunk(buf, nextAddr, []int32{int32(sylID)}, 0,
				wantWords, found, resolveText, resolveSyl)
		}

		if len(found) == len(wantWords) {
			break
		}
	}

	for word, wantQP := range wantWords {
		raw, ok := found[word]
		if !ok {
			t.Errorf("word %q not found in first %d syllables", word, limit)
			continue
		}
		gotQP := convertToQuanpin(raw)
		if gotQP != wantQP {
			t.Errorf("%s: raw=%q, got=%q, want=%q", word, raw, gotQP, wantQP)
		} else {
			t.Logf("%s: raw=%q → %s OK", word, raw, gotQP)
		}
	}
}

func scanTrunk(
	b *buffer, addr int64, prefix []int32, depth int,
	wantWords map[string]string, found map[string]string,
	resolveText func(int32) string, resolveSyl func(int32) string,
) {
	saved := b.pos
	defer func() { b.pos = saved }()

	b.pos = addr
	arrSize := b.readU32()

	if depth < 2 {
		for i := uint32(0); i < arrSize; i++ {
			key := b.readI32()
			nodeEntries := b.readEntryList(resolveText)
			nextOff := b.readI32()

			code := append(append([]int32{}, prefix...), key)
			codeStr := codeToString(code, resolveSyl)
			for _, e := range nodeEntries {
				if _, want := wantWords[e.text]; want {
					if _, ok := found[e.text]; !ok {
						found[e.text] = codeStr
					}
				}
			}

			if nextOff != 0 {
				nextAddr := b.pos - 4 + int64(nextOff)
				scanTrunk(b, nextAddr, code, depth+1,
					wantWords, found, resolveText, resolveSyl)
			}
			if len(found) == len(wantWords) {
				return
			}
		}
	} else {
		for i := uint32(0); i < arrSize; i++ {
			extraSize := b.readU32()
			extraAt := b.pos
			extraOff := b.readI32()
			textID := b.readI32()
			b.pos += 4 // skip weight

			var extra []int32
			if extraSize > 0 && extraOff != 0 {
				s2 := b.pos
				b.pos = extraAt + int64(extraOff)
				extra = make([]int32, extraSize)
				for j := uint32(0); j < extraSize; j++ {
					extra[j] = b.readI32()
				}
				b.pos = s2
			}

			text := resolveText(textID)
			if _, want := wantWords[text]; want {
				code := append(append([]int32{}, prefix...), extra...)
				if _, ok := found[text]; !ok {
					found[text] = codeToString(code, resolveSyl)
				}
			}
			if len(found) == len(wantWords) {
				return
			}
		}
	}
}
