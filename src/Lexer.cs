using System.Text;

namespace MiniMaxon;

public enum T {
  Ident, Int, Str, Quote, Newline, Eof,
  LParen, RParen, LBrace, RBrace, LBracket, RBracket,
  Comma, Colon, Dot, Plus, Minus, Star, Slash, Assign, EqEq, NotEq, Lt, Le, Gt, Ge, At,
}

public abstract record StrPart;
public sealed record TextPart(string Text) : StrPart;
public sealed record ExprPart(string Source, int Line, int Col) : StrPart;

public readonly record struct Token(T Kind, string Text, int Line, int Col, object? Payload = null) {
  public Int128 IntValue => (Int128)Payload!;
  public List<StrPart>? Parts => Payload as List<StrPart>;

  public bool Is(string word) => Kind == T.Ident && Text == word;
}

public sealed class Lexer(string file, string src, int startLine = 1, int startCol = 1, List<Token>? buffer = null) {
  static readonly Int128 U64Max = ulong.MaxValue;

  int pos;
  int line = startLine;
  int col = startCol;
  int depth;
  readonly List<Token> toks = Fresh(buffer);
  readonly Dictionary<string, string> pool = [];

  static List<Token> Fresh(List<Token>? buffer) {
    if (buffer is null) {
      return [];
    }
    buffer.Clear();
    return buffer;
  }

  string Intern(ReadOnlySpan<char> text) {
    var lookup = pool.GetAlternateLookup<ReadOnlySpan<char>>();
    if (lookup.TryGetValue(text, out var interned)) {
      return interned;
    }
    var s = text.ToString();
    pool[s] = s;
    return s;
  }

  public List<Token> Lex() {
    while (true) {
      SkipSpaceAndComments();
      if (pos >= src.Length) {
        break;
      }

      var c = src[pos];
      var l = line;
      var cl = col;

      if (c == '\n') {
        Advance();
        if (depth == 0 && toks.Count > 0 && toks[^1].Kind != T.Newline) {
          toks.Add(new Token(T.Newline, "\\n", l, cl));
        }
        continue;
      }

      if (char.IsAsciiLetter(c) || c == '_') {
        var start = pos;
        while (pos < src.Length && (char.IsAsciiLetterOrDigit(src[pos]) || src[pos] == '_')) {
          Advance();
        }
        toks.Add(new Token(T.Ident, Intern(src.AsSpan(start, pos - start)), l, cl));
        continue;
      }

      if (char.IsAsciiDigit(c)) {
        LexNumber(l, cl);
        continue;
      }

      if (c == '"') {
        LexString(l, cl);
        continue;
      }

      if (c == '\'') {
        LexQuote(l, cl);
        continue;
      }

      LexPunct(c, l, cl);
    }

    if (toks.Count > 0 && toks[^1].Kind != T.Newline) {
      toks.Add(new Token(T.Newline, "\\n", line, col));
    }
    toks.Add(new Token(T.Eof, "<eof>", line, col));
    return toks;
  }

  CompileError Error(string code, string msg, int l, int c) => new(code, msg, file, l, c);

  void Advance() {
    if (src[pos] == '\n') {
      line++;
      col = 1;
    } else {
      col++;
    }
    pos++;
  }

  char Peek(int ahead = 0) => pos + ahead < src.Length ? src[pos + ahead] : '\0';

  void SkipSpaceAndComments() {
    while (pos < src.Length) {
      var c = src[pos];
      if (c == ' ' || c == '\t' || c == '\r') {
        Advance();
      } else if (c == '/' && Peek(1) == '/') {
        while (pos < src.Length && src[pos] != '\n') {
          Advance();
        }
      } else if (c == '/' && Peek(1) == '*') {
        var l = line;
        var cl = col;
        Advance();
        Advance();
        while (pos < src.Length && !(src[pos] == '*' && Peek(1) == '/')) {
          Advance();
        }
        if (pos >= src.Length) {
          throw Error(Err.UnexpectedToken, "unterminated block comment", l, cl);
        }
        Advance();
        Advance();
      } else {
        return;
      }
    }
  }

  void LexNumber(int l, int cl) {
    var radix = 10;
    if (src[pos] == '0' && (Peek(1) is 'x' or 'b' or 'o')) {
      radix = Peek(1) switch { 'x' => 16, 'b' => 2, _ => 8 };
      Advance();
      Advance();
    }

    var start = pos;
    Int128 value = 0;
    var digits = 0;
    while (pos < src.Length && (char.IsAsciiHexDigit(src[pos]) || src[pos] == '_')) {
      var ch = src[pos];
      if (ch == '_') {
        Advance();
        continue;
      }
      var d = char.IsAsciiDigit(ch) ? ch - '0' : char.ToLowerInvariant(ch) - 'a' + 10;
      if (d >= radix) {
        break;
      }
      value = value * radix + d;
      if (value > U64Max) {
        throw Error(Err.ExpressionError, "integer literal is larger than u64.max", l, cl);
      }
      digits++;
      Advance();
    }

    if (digits == 0) {
      throw Error(Err.ExpressionError, "malformed integer literal", l, cl);
    }
    if (pos < src.Length && src[pos] == '.' && char.IsAsciiDigit(Peek(1))) {
      throw Error(Err.Unsupported, "float literals are not supported by minimaxon", l, cl);
    }

    toks.Add(new Token(T.Int, Intern(src.AsSpan(start, pos - start)), l, cl, value));
  }

  char ReadEscape(int l, int cl) {
    Advance();
    var e = Peek();
    Advance();
    switch (e) {
      case 'n': return '\n';
      case 't': return '\t';
      case 'r': return '\r';
      case '0': return '\0';
      case '\\': return '\\';
      case '"': return '"';
      case '\'': return '\'';
      case '{': return '{';
      case '}': return '}';
      case 'x': return (char)ReadHex(2, l, cl);
      case 'u': return (char)ReadHex(4, l, cl);
      default: throw Error(Err.UnexpectedToken, $"unknown escape sequence '\\{e}'", l, cl);
    }
  }

  int ReadHex(int n, int l, int cl) {
    var v = 0;
    for (var i = 0; i < n; i++) {
      var ch = Peek();
      if (!char.IsAsciiHexDigit(ch)) {
        throw Error(Err.UnexpectedToken, "malformed hex escape", l, cl);
      }
      v = v * 16 + Convert.ToInt32(ch.ToString(), 16);
      Advance();
    }
    return v;
  }

  void LexString(int l, int cl) {
    Advance();
    var parts = new List<StrPart>();
    var text = new StringBuilder();

    while (true) {
      if (pos >= src.Length || src[pos] == '\n') {
        throw Error(Err.UnexpectedToken, "unterminated string literal", l, cl);
      }
      var c = src[pos];
      if (c == '"') {
        Advance();
        break;
      }
      if (c == '\\') {
        text.Append(ReadEscape(l, cl));
        continue;
      }
      if (c == '}') {
        throw Error(Err.UnexpectedToken, "a '}' in a string is written '\\}'", line, col);
      }
      if (c == '{') {
        if (text.Length > 0) {
          parts.Add(new TextPart(text.ToString()));
          text.Clear();
        }
        Advance();
        var el = line;
        var ec = col;
        var start = pos;
        var nest = 0;
        var parens = 0;
        while (pos < src.Length && src[pos] != '\n' && !(src[pos] == '}' && nest == 0)) {
          if (src[pos] == '{') {
            nest++;
          } else if (src[pos] == '}') {
            nest--;
          } else if (src[pos] is '(' or '[') {
            parens++;
          } else if (src[pos] is ')' or ']') {
            parens--;
          } else if (src[pos] == ':' && parens == 0 && nest == 0) {
            throw Error(Err.Unsupported, "format specifiers are not supported by minimaxon", line, col);
          } else if (src[pos] == '"') {
            Advance();
            while (pos < src.Length && src[pos] != '"' && src[pos] != '\n') {
              if (src[pos] == '\\') {
                Advance();
              }
              Advance();
            }
          }
          Advance();
        }
        if (pos >= src.Length || src[pos] != '}') {
          throw Error(Err.UnexpectedToken, "unterminated interpolation in string literal", el, ec);
        }
        parts.Add(new ExprPart(src[start..pos], el, ec));
        Advance();
        continue;
      }
      text.Append(c);
      Advance();
    }

    if (text.Length > 0 || parts.Count == 0) {
      parts.Add(new TextPart(text.ToString()));
    }
    toks.Add(new Token(T.Str, "\"...\"", l, cl, parts));
  }

  void LexQuote(int l, int cl) {
    Advance();
    var start = pos;
    while (pos < src.Length && src[pos] != '\'' && src[pos] != '\n') {
      Advance();
    }
    if (pos >= src.Length || src[pos] != '\'') {
      throw Error(Err.UnexpectedToken, "unterminated label", l, cl);
    }
    var text = Intern(src.AsSpan(start, pos - start));
    Advance();
    toks.Add(new Token(T.Quote, text, l, cl));
  }

  void LexPunct(char c, int l, int cl) {
    T kind;
    var len = 1;
    switch (c) {
      case '(': kind = T.LParen; depth++; break;
      case ')': kind = T.RParen; depth = Math.Max(0, depth - 1); break;
      case '{': kind = T.LBrace; depth++; break;
      case '}': kind = T.RBrace; depth = Math.Max(0, depth - 1); break;
      case '[': kind = T.LBracket; depth++; break;
      case ']': kind = T.RBracket; depth = Math.Max(0, depth - 1); break;
      case ',': kind = T.Comma; break;
      case ':': kind = T.Colon; break;
      case '.': kind = T.Dot; break;
      case '+': kind = T.Plus; break;
      case '-': kind = T.Minus; break;
      case '*': kind = T.Star; break;
      case '/': kind = T.Slash; break;
      case '@': kind = T.At; break;
      case '=':
        if (Peek(1) == '=') { kind = T.EqEq; len = 2; } else { kind = T.Assign; }
        break;
      case '!':
        if (Peek(1) != '=') {
          throw Error(Err.UnexpectedToken, "unexpected character '!'", l, cl);
        }
        kind = T.NotEq;
        len = 2;
        break;
      case '<':
        if (Peek(1) == '=') { kind = T.Le; len = 2; } else { kind = T.Lt; }
        break;
      case '>':
        if (Peek(1) == '=') { kind = T.Ge; len = 2; } else { kind = T.Gt; }
        break;
      case '#':
        throw Error(Err.Unsupported, "conditional compilation is not supported by minimaxon", l, cl);
      default:
        throw Error(Err.UnexpectedToken, $"unexpected character '{c}'", l, cl);
    }

    var text = Intern(src.AsSpan(pos, len));
    for (var i = 0; i < len; i++) {
      Advance();
    }
    toks.Add(new Token(kind, text, l, cl));
  }
}
