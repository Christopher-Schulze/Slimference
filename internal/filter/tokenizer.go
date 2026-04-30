// Package filter — shell command tokenizer for the §4.2 rewrite engine.
package filter

import "strings"

// TokenKind classifies a shell token for the rewrite engine.
type TokenKind int

const (
	TokenArg      TokenKind = iota // regular argument or bare word
	TokenOperator                  // && || ;
	TokenPipe                      // |
	TokenRedirect                  // > >> 2>&1 &> < << <<<
	TokenShellism                  // * ? $var ${var} $( ) $(( )) backticks
)

// ParsedToken is one token produced by tokenize.
type ParsedToken struct {
	Kind   TokenKind
	Value  string
	Offset int // byte offset in original string
}

// tokenize splits a shell command line into typed tokens.
// It correctly handles:
//   - Single-quoted strings ('...')  — no escapes inside
//   - Double-quoted strings ("...")  — \" \\ \$ escape sequences
//   - Backslash escapes outside quotes
//   - Operators: && || ; (TokenOperator)
//   - Pipe: | (TokenPipe)
//   - Redirects: > >> < 2>&1 &> &>> <<< (TokenRedirect)
//   - Shellisms: * ? $VAR ${VAR} $(...) $((...)) backticks (TokenShellism)
func tokenize(input string) []ParsedToken {
	var tokens []ParsedToken
	i := 0
	n := len(input)

	skipSpace := func() {
		for i < n && (input[i] == ' ' || input[i] == '\t') {
			i++
		}
	}

	for i < n {
		skipSpace()
		if i >= n {
			break
		}

		start := i
		ch := input[i]

		// Two-char operators first
		if i+1 < n {
			two := input[i : i+2]
			switch two {
			case "&&", "||":
				tokens = append(tokens, ParsedToken{Kind: TokenOperator, Value: two, Offset: start})
				i += 2
				continue
			case ">>", "&>", "<<":
				// check for <<< (heredoc string)
				if two == "<<" && i+2 < n && input[i+2] == '<' {
					tokens = append(tokens, ParsedToken{Kind: TokenRedirect, Value: "<<<", Offset: start})
					i += 3
					continue
				}
				// check for &>>
				if two == "&>" && i+2 < n && input[i+2] == '>' {
					tokens = append(tokens, ParsedToken{Kind: TokenRedirect, Value: "&>>", Offset: start})
					i += 3
					continue
				}
				tokens = append(tokens, ParsedToken{Kind: TokenRedirect, Value: two, Offset: start})
				i += 2
				continue
			}
		}

		switch ch {
		case ';':
			tokens = append(tokens, ParsedToken{Kind: TokenOperator, Value: ";", Offset: start})
			i++
		case '|':
			tokens = append(tokens, ParsedToken{Kind: TokenPipe, Value: "|", Offset: start})
			i++
		case '>', '<':
			tokens = append(tokens, ParsedToken{Kind: TokenRedirect, Value: string(ch), Offset: start})
			i++
		default:
			// Build a word token.
			var sb strings.Builder
			kind := TokenArg
			for i < n {
				c := input[i]
				if c == ' ' || c == '\t' {
					break
				}
				if c == '\'' {
					// single-quoted: consume until closing '
					i++
					for i < n && input[i] != '\'' {
						sb.WriteByte(input[i])
						i++
					}
					if i < n {
						i++ // closing '
					}
					continue
				}
				if c == '"' {
					// double-quoted
					i++
					for i < n && input[i] != '"' {
						if input[i] == '\\' && i+1 < n {
							next := input[i+1]
							if next == '"' || next == '\\' || next == '$' || next == '`' {
								i++
								sb.WriteByte(input[i])
								i++
								continue
							}
						}
						if input[i] == '$' || input[i] == '`' {
							kind = TokenShellism
						}
						sb.WriteByte(input[i])
						i++
					}
					if i < n {
						i++ // closing "
					}
					continue
				}
				if c == '\\' && i+1 < n {
					i++
					sb.WriteByte(input[i])
					i++
					continue
				}
				if c == '$' {
					kind = TokenShellism
					sb.WriteByte(c)
					i++
					// consume $(...), ${...}, $((...))
					if i < n && input[i] == '(' {
						depth := 1
						sb.WriteByte(input[i])
						i++
						for i < n && depth > 0 {
							if input[i] == '(' {
								depth++
							} else if input[i] == ')' {
								depth--
							}
							sb.WriteByte(input[i])
							i++
						}
					} else if i < n && input[i] == '{' {
						sb.WriteByte(input[i])
						i++
						for i < n && input[i] != '}' {
							sb.WriteByte(input[i])
							i++
						}
						if i < n {
							sb.WriteByte(input[i])
							i++
						}
					}
					continue
				}
				if c == '`' {
					kind = TokenShellism
					sb.WriteByte(c)
					i++
					for i < n && input[i] != '`' {
						sb.WriteByte(input[i])
						i++
					}
					if i < n {
						sb.WriteByte(input[i])
						i++
					}
					continue
				}
				if c == '*' || c == '?' {
					kind = TokenShellism
					sb.WriteByte(c)
					i++
					continue
				}
				// Redirect or operator embedded in word (e.g. 2>&1)?
				if c == '2' && i+3 < n && input[i+1] == '>' && input[i+2] == '&' && input[i+3] == '1' {
					// flush word so far if any, then emit redirect
					if sb.Len() > 0 {
						tokens = append(tokens, ParsedToken{Kind: kind, Value: sb.String(), Offset: start})
						sb.Reset()
					}
					tokens = append(tokens, ParsedToken{Kind: TokenRedirect, Value: "2>&1", Offset: i})
					i += 4
					// reset start for any continuation
					start = i
					kind = TokenArg
					continue
				}
				if c == '>' || c == '<' || c == ';' || c == '|' || c == '&' {
					break
				}
				sb.WriteByte(c)
				i++
			}
			if sb.Len() > 0 {
				tokens = append(tokens, ParsedToken{Kind: kind, Value: sb.String(), Offset: start})
			}
		}
	}
	return tokens
}
