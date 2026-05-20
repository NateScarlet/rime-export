package main

import (
	"testing"
)

// Test cases: raw code from table.bin → expected full pinyin
// Verified against actual Python extraction output
var testCases = []struct {
	raw  string
	want string
}{
	// These codes come from the actual table.bin syllabary
	{"dj[kd", "dan"},             // 丹
	{"hg[xd", "heng"},            // 恒
	{"qi[av", "qi"},              // 七
	{"vc[dk", "zhao"},            // 召
	{"ug[yt", "sheng"},           // 圣
	{"wu[kw", "wu"},              // 呜
	{"bo[rb", "bo"},              // 伯
	{"hr[kr", "huan"},            // 唤
	{"yi[aa", "yi"},              // 一
	{"ff[rf", "fen"},             // 份: f=en
	{"jm[bd", "jian"},            // 剪: m=ian
	{"bc[fy", "bao"},             // 报: c=ao
	{"xk[ou", "xing"},            // 星: k=ing
	{"qs[bg", "qiong"},           // 穹: s=iong
	{"tp[jf", "tie"},             // 铁: p=ie
	{"dc[zz", "dao"},             // 道: c=ao
	{"sj[ae", "san"},             // 三: j=an
	{"yt[ke", "yue"},             // 月: t=ue
	// Multi-char words
	{"dj[kd/hg[xd", "dan|heng"},                           // 丹恒
	{"qi[av/ug[yt/vc[dk/hr[kr", "qi|sheng|zhao|huan"},     // 七圣召唤
	{"wu[kw/wu[kw/bo[rb", "wu|wu|bo"},                     // 呜呜伯
}

func TestConvertToQuanpin(t *testing.T) {
	for _, tc := range testCases {
		got := convertToQuanpin(tc.raw)
		if got != tc.want {
			t.Errorf("convertToQuanpin(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// Individual 双拼 codes stripped of auxiliary parts
var spTests = []struct {
	sp   string
	want string
}{
	{"dj", "dan"},
	{"hg", "heng"},
	{"qi", "qi"},
	{"vc", "zhao"},
	{"ug", "sheng"},
	{"wu", "wu"},
	{"bo", "bo"},
	{"hr", "huan"},
	{"yi", "yi"},
	{"ff", "fen"},
	{"jm", "jian"},
	{"bc", "bao"},
	{"xk", "xing"},
	{"qs", "qiong"},
	{"tp", "tie"},
	{"dc", "dao"},
	{"sj", "san"},
	{"yt", "yue"},
}

func TestSpToQuanpin(t *testing.T) {
	for _, tc := range spTests {
		got := spToQuanpin(tc.sp)
		if got != tc.want {
			t.Errorf("spToQuanpin(%q) = %q, want %q", tc.sp, got, tc.want)
		}
	}
}
