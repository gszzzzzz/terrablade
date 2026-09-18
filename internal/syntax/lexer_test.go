package syntax

import (
	"bytes"
	"reflect"
	"testing"
)

type tokenText struct {
	kind TokenKind
	text string
}

func TestLexTokens(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []tokenText
	}{
		{
			"empty",
			"",
			nil,
		},
		{
			"BOM at start",
			"\uFEFFa=1",
			[]tokenText{
				{BOM, "\uFEFF"},
				{Identifier, "a"},
				{Equal, "="},
				{Number, "1"},
			},
		},
		{
			"BOM placement left to parser",
			"a\uFEFF\uFEFFb\n\uFEFF",
			[]tokenText{
				{Identifier, "a"},
				{BOM, "\uFEFF"},
				{BOM, "\uFEFF"},
				{Identifier, "b"},
				{Newline, "\n"},
				{BOM, "\uFEFF"},
			},
		},
		{
			"BOM stays comment content",
			"#\uFEFF\n//\uFEFF\r\n/*\uFEFF*/",
			[]tokenText{
				{LineComment, "#\uFEFF"},
				{Newline, "\n"},
				{LineComment, "//\uFEFF"},
				{Newline, "\r\n"},
				{BlockComment, "/*\uFEFF*/"},
			},
		},
		{
			"namespaced function",
			"provider::aws::arn_parse(x)",
			[]tokenText{
				{Identifier, "provider"},
				{DoubleColon, "::"},
				{Identifier, "aws"},
				{DoubleColon, "::"},
				{Identifier, "arn_parse"},
				{OpenParen, "("},
				{Identifier, "x"},
				{CloseParen, ")"},
			},
		},
		{
			"colon longest match",
			"::::: : :",
			[]tokenText{
				{DoubleColon, "::"},
				{DoubleColon, "::"},
				{Colon, ":"},
				{Whitespace, " "},
				{Colon, ":"},
				{Whitespace, " "},
				{Colon, ":"},
			},
		},
		{
			"trivia",
			" \t\t \n\r\n\n",
			[]tokenText{
				{Whitespace, " \t\t "},
				{Newline, "\n"},
				{Newline, "\r\n"},
				{Newline, "\n"},
			},
		},
		{
			"line comments",
			"a#one\nb//two\r\n#eof",
			[]tokenText{
				{Identifier, "a"},
				{LineComment, "#one"},
				{Newline, "\n"},
				{Identifier, "b"},
				{LineComment, "//two"},
				{Newline, "\r\n"},
				{LineComment, "#eof"},
			},
		},
		{
			"comment endings",
			"#\rstill comment\r\n//\n",
			[]tokenText{
				{LineComment, "#\rstill comment"},
				{Newline, "\r\n"},
				{LineComment, "//"},
				{Newline, "\n"},
			},
		},
		{
			"block comment",
			"a/*\r\n# // \" << /* x*/b*/",
			[]tokenText{
				{Identifier, "a"},
				{BlockComment, "/*\r\n# // \" << /* x*/"},
				{Identifier, "b"},
				{Star, "*"},
				{Slash, "/"},
			},
		},
		{
			"unicode",
			"_a-2 한글 e\u0301 é \u2118x x\u200Cz \U00010940 \U00011DB0",
			[]tokenText{
				{Identifier, "_a-2"},
				{Whitespace, " "},
				{Identifier, "한글"},
				{Whitespace, " "},
				{Identifier, "e\u0301"},
				{Whitespace, " "},
				{Identifier, "é"},
				{Whitespace, " "},
				{Identifier, "\u2118x"},
				{Whitespace, " "},
				{Identifier, "x\u200Cz"},
				{Whitespace, " "},
				{Identifier, "\U00010940"},
				{Whitespace, " "},
				{Identifier, "\U00011DB0"},
			},
		},
		{
			"keywords stay identifiers",
			"for true false null in if",
			[]tokenText{
				{Identifier, "for"},
				{Whitespace, " "},
				{Identifier, "true"},
				{Whitespace, " "},
				{Identifier, "false"},
				{Whitespace, " "},
				{Identifier, "null"},
				{Whitespace, " "},
				{Identifier, "in"},
				{Whitespace, " "},
				{Identifier, "if"},
			},
		},
		{
			"hyphen ambiguity",
			"a-1 a - 1 -3",
			[]tokenText{
				{Identifier, "a-1"},
				{Whitespace, " "},
				{Identifier, "a"},
				{Whitespace, " "},
				{Minus, "-"},
				{Whitespace, " "},
				{Number, "1"},
				{Whitespace, " "},
				{Minus, "-"},
				{Number, "3"},
			},
		},
		{
			"numbers",
			"0 012 1.25 1e3 1E+3 1.5e-20",
			[]tokenText{
				{Number, "0"},
				{Whitespace, " "},
				{Number, "012"},
				{Whitespace, " "},
				{Number, "1.25"},
				{Whitespace, " "},
				{Number, "1e3"},
				{Whitespace, " "},
				{Number, "1E+3"},
				{Whitespace, " "},
				{Number, "1.5e-20"},
			},
		},
		{
			"numeric boundaries",
			"1. .5 1e+ 1e- 0xFF 1... foo.0.0",
			[]tokenText{
				{Number, "1"},
				{Dot, "."},
				{Whitespace, " "},
				{Dot, "."},
				{Number, "5"},
				{Whitespace, " "},
				{Number, "1"},
				{Identifier, "e"},
				{Plus, "+"},
				{Whitespace, " "},
				{Number, "1"},
				{Identifier, "e-"},
				{Whitespace, " "},
				{Number, "0"},
				{Identifier, "xFF"},
				{Whitespace, " "},
				{Number, "1"},
				{Ellipsis, "..."},
				{Whitespace, " "},
				{Identifier, "foo"},
				{Dot, "."},
				{Number, "0.0"},
			},
		},
		{
			"operators",
			"+-*/%&&||!==!=<>>===>:?.,...{}[]()",
			[]tokenText{
				{Plus, "+"},
				{Minus, "-"},
				{Star, "*"},
				{Slash, "/"},
				{Percent, "%"},
				{And, "&&"},
				{Or, "||"},
				{NotEqual, "!="},
				{Equal, "="},
				{NotEqual, "!="},
				{Less, "<"},
				{Greater, ">"},
				{GreaterEqual, ">="},
				{EqualEqual, "=="},
				{Greater, ">"},
				{Colon, ":"},
				{Question, "?"},
				{Dot, "."},
				{Comma, ","},
				{Ellipsis, "..."},
				{OpenBrace, "{"},
				{CloseBrace, "}"},
				{OpenBracket, "["},
				{CloseBracket, "]"},
				{OpenParen, "("},
				{CloseParen, ")"},
			},
		},
		{
			"longest matches",
			"=> == = != ! <= < >= > ....",
			[]tokenText{
				{Arrow, "=>"},
				{Whitespace, " "},
				{EqualEqual, "=="},
				{Whitespace, " "},
				{Equal, "="},
				{Whitespace, " "},
				{NotEqual, "!="},
				{Whitespace, " "},
				{Bang, "!"},
				{Whitespace, " "},
				{LessEqual, "<="},
				{Whitespace, " "},
				{Less, "<"},
				{Whitespace, " "},
				{GreaterEqual, ">="},
				{Whitespace, " "},
				{Greater, ">"},
				{Whitespace, " "},
				{Ellipsis, "..."},
				{Dot, "."},
			},
		},
		{
			"body",
			"locals {\n a=[1,foo.bar]\n}\n",
			[]tokenText{
				{Identifier, "locals"},
				{Whitespace, " "},
				{OpenBrace, "{"},
				{Newline, "\n"},
				{Whitespace, " "},
				{Identifier, "a"},
				{Equal, "="},
				{OpenBracket, "["},
				{Number, "1"},
				{Comma, ","},
				{Identifier, "foo"},
				{Dot, "."},
				{Identifier, "bar"},
				{CloseBracket, "]"},
				{Newline, "\n"},
				{CloseBrace, "}"},
				{Newline, "\n"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			result := lex(source)
			assertPartition(t, source, result)
			if len(result.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
			}
			var got []tokenText
			for _, token := range result.Tokens[:len(result.Tokens)-1] {
				got = append(got, tokenText{token.Kind(), string(source[token.Span().Start:token.Span().End])})
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("tokens = %#v\nwant %#v", got, test.want)
			}
		})
	}
}

func TestLexDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []Diagnostic
	}{
		{"bare CR", "a\rb", []Diagnostic{{InvalidCharacter, Span{1, 2}}}},
		{
			"invalid characters",
			"&|;@'\\\x00\u00A0",
			[]Diagnostic{
				{InvalidCharacter, Span{0, 1}},
				{InvalidCharacter, Span{1, 2}},
				{InvalidCharacter, Span{2, 3}},
				{InvalidCharacter, Span{3, 4}},
				{InvalidCharacter, Span{4, 5}},
				{InvalidCharacter, Span{5, 6}},
				{InvalidCharacter, Span{6, 7}},
				{InvalidCharacter, Span{7, 9}},
			},
		},
		{
			"invalid UTF8",
			"a\xff\xc0\x80b",
			[]Diagnostic{
				{InvalidUTF8, Span{1, 2}},
				{InvalidUTF8, Span{2, 3}},
				{InvalidUTF8, Span{3, 4}},
			},
		},
		{"valid replacement rune", "\uFFFD", []Diagnostic{{InvalidCharacter, Span{0, 3}}}},
		{"encoding inside line comment", "#\xff\uFEFF\n", []Diagnostic{{InvalidUTF8, Span{1, 2}}}},
		{"encoding inside block comment", "/*\xff*/", []Diagnostic{{InvalidUTF8, Span{2, 3}}}},
		{"unterminated comment", "/*x", []Diagnostic{{UnterminatedBlockComment, Span{0, 3}}}},
		{
			"ordered errors",
			"/*\xff",
			[]Diagnostic{
				{UnterminatedBlockComment, Span{0, 3}},
				{InvalidUTF8, Span{2, 3}},
			},
		},
		{"continuation cannot start", "\u0301a", []Diagnostic{{InvalidCharacter, Span{0, 2}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := lex([]byte(test.source))
			assertPartition(t, []byte(test.source), result)
			if !reflect.DeepEqual(result.Diagnostics, test.want) {
				t.Errorf("diagnostics = %+v, want %+v", result.Diagnostics, test.want)
			}
		})
	}
}

func TestLexEveryByte(t *testing.T) {
	for value := range 256 {
		source := []byte{byte(value)}
		assertPartition(t, source, lex(source))
	}
}

// Boundaries were checked with HCL v2.25.0 LexExpression. Numeric validity is a
// parser concern, so malformed candidates still have no lexical diagnostic.
func TestLexNumericCandidates(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []tokenText
	}{
		{
			"trailing dot",
			"1.",
			[]tokenText{
				{Number, "1"},
				{Dot, "."},
			},
		},
		{
			"attribute named e",
			"1.e",
			[]tokenText{
				{Number, "1"},
				{Dot, "."},
				{Identifier, "e"},
			},
		},
		{
			"empty fraction with exponent",
			"1.e2",
			[]tokenText{
				{Number, "1.e2"},
			},
		},
		{
			"empty fraction with signed exponent",
			"1.e+2",
			[]tokenText{
				{Number, "1.e+2"},
			},
		},
		{
			"subtraction after exponent",
			"1.e2-foo",
			[]tokenText{
				{Number, "1.e2"},
				{Minus, "-"},
				{Identifier, "foo"},
			},
		},
		{
			"ordinary attribute name",
			"1.foo",
			[]tokenText{
				{Number, "1"},
				{Dot, "."},
				{Identifier, "foo"},
			},
		},
		{
			"multiple decimal points",
			"1.0.2",
			[]tokenText{
				{Number, "1.0.2"},
			},
		},
		{
			"legacy index with exponent and decimal point",
			"foo.0e1.0",
			[]tokenText{
				{Identifier, "foo"},
				{Dot, "."},
				{Number, "0e1.0"},
			},
		},
		{
			"space separates legacy indices",
			"foo.0 .0",
			[]tokenText{
				{Identifier, "foo"},
				{Dot, "."},
				{Number, "0"},
				{Whitespace, " "},
				{Dot, "."},
				{Number, "0"},
			},
		},
		{
			"trailing ellipsis",
			"1...",
			[]tokenText{
				{Number, "1"},
				{Ellipsis, "..."},
			},
		},
		{
			"interior ellipsis",
			"1...2",
			[]tokenText{
				{Number, "1...2"},
			},
		},
		{
			"incomplete positive exponent is attribute and operator",
			"1.e+",
			[]tokenText{
				{Number, "1"},
				{Dot, "."},
				{Identifier, "e"},
				{Plus, "+"},
			},
		},
		{
			"incomplete negative exponent is hyphenated attribute",
			"1.e-",
			[]tokenText{
				{Number, "1"},
				{Dot, "."},
				{Identifier, "e-"},
			},
		},
		{
			"number followed by identifier",
			"1.e2foo",
			[]tokenText{
				{Number, "1.e2"},
				{Identifier, "foo"},
			},
		},
		{
			"repeated exponent followed by identifier",
			"1e1e2foo",
			[]tokenText{
				{Number, "1e1e2"},
				{Identifier, "foo"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			result := lex(source)
			assertPartition(t, source, result)
			if len(result.Diagnostics) != 0 {
				t.Fatalf("unexpected lexical diagnostics: %+v", result.Diagnostics)
			}
			var got []tokenText
			for _, token := range result.Tokens[:len(result.Tokens)-1] {
				got = append(got, tokenText{token.Kind(), string(source[token.Span().Start:token.Span().End])})
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tokens = %+v, want %+v", got, test.want)
			}
		})
	}
}

func FuzzLex(f *testing.F) {
	f.Add([]byte("\uFEFFa\uFEFF#\uFEFF\n/*\uFEFF*/"))
	for _, source := range []string{
		`"${~ {a="${x}"} ~}"`,
		`"%{~for x in xs~}${x}%{~endfor~}"`,
		`"$${escaped} %%{escaped} \uD800 \U00110000 \q"`,
		"\"${# }\r\n/* } */x}\"",
		"<<-OUT\r\nraw ${<<IN\ninner\nIN\n}\r\n \tOUT \t\r\n",
		"<<E\n%{if x}${\"${y}\"}%{endif}\nE\n",
		"<<E\nE", "<<-E", "\"${\"${", "<<E\n${<<F\n",
		"\"\\\xff${\uFEFFx}\"", "<<E\n\xff\uFEFF\nE\n",
	} {
		f.Add([]byte(source))
	}
	for _, source := range []string{
		"",
		"x = 1\r\n",
		"a-1=1.2E-3",
		"/* unclosed",
		"#\xff\n\xfe",
		"\"${{a=1}}\"",
		"<<-EOT\n${x}\nEOT",
		"한국어 e\u0301 \U00010940",
		"\x00\xc0\xaf\xed\xa0\x80",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		original := bytes.Clone(source)
		first := lex(source)
		assertPartition(t, source, first)
		if !bytes.Equal(source, original) {
			t.Fatal("lex modified source")
		}
		if second := lex(source); !reflect.DeepEqual(first, second) {
			t.Fatal("lex is not deterministic")
		}
	})
}

func assertPartition(t *testing.T, source []byte, result lexResult) {
	t.Helper()
	if len(result.Tokens) == 0 {
		t.Fatal("missing EOF")
	}
	end := 0
	var reconstructed []byte
	for i, token := range result.Tokens {
		span := token.Span()
		if span.Start != end || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid partition at token %d: %+v, previous end %d", i, token, end)
		}
		if token.Kind() == EOF {
			if i != len(result.Tokens)-1 || span.Start != len(source) || span.End != len(source) {
				t.Fatalf("invalid EOF: %+v", token)
			}
		} else if span.Start == span.End {
			t.Fatalf("empty non-EOF token: %+v", token)
		}
		if token.Kind() == BOM && !bytes.Equal(source[span.Start:span.End], []byte("\uFEFF")) {
			t.Fatalf("BOM token does not span exactly one UTF-8 BOM: %+v", token)
		}
		reconstructed = append(reconstructed, source[span.Start:span.End]...)
		end = span.End
	}
	if result.Tokens[len(result.Tokens)-1].Kind() != EOF || end != len(source) {
		t.Fatal("missing final EOF or source bytes")
	}
	if !bytes.Equal(reconstructed, source) {
		t.Fatal("tokens do not reconstruct source")
	}
	lastStart := -1
	for _, diagnostic := range result.Diagnostics {
		span := diagnostic.Span
		if span.Start < lastStart || span.Start < 0 || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid diagnostic span/order: %+v", diagnostic)
		}
		lastStart = span.Start
	}
}
