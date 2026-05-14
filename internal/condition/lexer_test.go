package condition

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func tokenKindsOf(toks []token) []tokenKind {
	out := make([]tokenKind, 0, len(toks))
	for _, t := range toks {
		out = append(out, t.kind)
	}
	return out
}

func TestLexerKeywords(t *testing.T) {
	toks, err := lex("AND or NOT MATCHES exists Contains IN")
	require.NoError(t, err)
	require.Equal(t, []tokenKind{
		tkAnd, tkOr, tkNot, tkMatches, tkExistsKW, tkContains, tkIn, tkEOF,
	}, tokenKindsOf(toks))
}

func TestLexerSymbolicOps(t *testing.T) {
	toks, err := lex("= != < <= > >= ~ ? ! & |")
	require.NoError(t, err)
	require.Equal(t, []tokenKind{
		tkEq, tkNeq, tkLt, tkLte, tkGt, tkGte, tkMatches, tkExistsSym, tkNot,
		tkAnd, tkOr, tkEOF,
	}, tokenKindsOf(toks))
}

func TestLexerEscapedStrings(t *testing.T) {
	toks, err := lex(`"a\"b" 'c\nd' "tab\there"`)
	require.NoError(t, err)
	require.Equal(t, `a"b`, toks[0].text)
	require.Equal(t, "c\nd", toks[1].text)
	require.Equal(t, "tab\there", toks[2].text)
}

func TestLexerNumbers(t *testing.T) {
	toks, err := lex("1 -42 3.14 1e3 1.5e-2 +7")
	require.NoError(t, err)
	require.Equal(t, int64(1), toks[0].intVal)
	require.True(t, toks[0].isInt)
	require.Equal(t, int64(-42), toks[1].intVal)
	require.True(t, toks[2].isFloat)
	require.InDelta(t, 3.14, toks[2].num, 1e-9)
	require.True(t, toks[3].isFloat)
	require.InDelta(t, 1000.0, toks[3].num, 1e-9)
	require.True(t, toks[4].isFloat)
	require.InDelta(t, 0.015, toks[4].num, 1e-9)
	require.Equal(t, int64(7), toks[5].intVal)
}

func TestLexerNestedArrays(t *testing.T) {
	toks, err := lex("[[1,2],[3]]")
	require.NoError(t, err)
	kinds := tokenKindsOf(toks)
	require.Equal(t, []tokenKind{
		tkLBrack, tkLBrack, tkNumber, tkComma, tkNumber, tkRBrack, tkComma,
		tkLBrack, tkNumber, tkRBrack, tkRBrack, tkEOF,
	}, kinds)
}

func TestLexerHashLikeIdent(t *testing.T) {
	toks, err := lex("hash=3f75728488a0e6892905f0db6a473382")
	require.NoError(t, err)
	require.Equal(t, tkIdent, toks[0].kind)
	require.Equal(t, "hash", toks[0].text)
	require.Equal(t, tkEq, toks[1].kind)
	require.Equal(t, tkIdent, toks[2].kind)
	require.Equal(t, "3f75728488a0e6892905f0db6a473382", toks[2].text)
}

func TestLexerBool(t *testing.T) {
	toks, err := lex("true FALSE True")
	require.NoError(t, err)
	require.Equal(t, tkBool, toks[0].kind)
	require.True(t, toks[0].boolVal)
	require.Equal(t, tkBool, toks[1].kind)
	require.False(t, toks[1].boolVal)
	require.Equal(t, tkBool, toks[2].kind)
	require.True(t, toks[2].boolVal)
}

func TestLexerUnterminatedString(t *testing.T) {
	_, err := lex(`"oops`)
	require.Error(t, err)
}
