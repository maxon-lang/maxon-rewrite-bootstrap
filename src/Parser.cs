namespace MiniMaxon;

public sealed class Parser(List<Token> toks, string file) {
  static readonly HashSet<string> Reserved = [
    "and", "as", "async", "await", "bool", "break", "continue", "countof", "cstring", "default", "else", "end",
    "enum", "export", "extends", "extension", "fallthrough", "false", "float", "for", "from", "function", "gives",
    "if", "ignore", "implements", "in", "int", "interface", "is", "let", "match", "mod", "not", "or", "otherwise",
    "panic", "public", "return", "returns", "self", "Self", "shl", "shr", "sizeof", "static", "then", "throw",
    "throws", "to", "true", "try", "type", "typealias", "union", "upto", "uses", "var", "where", "while", "with",
    "xor",
  ];

  static readonly HashSet<string> Capabilities = ["isolated", "immutable"];

  static readonly Dictionary<string, (Int128 Min, Int128 Max)> SizedTypes = new() {
    ["u8"] = (0, byte.MaxValue), ["u16"] = (0, ushort.MaxValue), ["u32"] = (0, uint.MaxValue), ["u64"] = (0, ulong.MaxValue),
    ["i8"] = (sbyte.MinValue, sbyte.MaxValue), ["i16"] = (short.MinValue, short.MaxValue),
    ["i32"] = (int.MinValue, int.MaxValue), ["i64"] = (long.MinValue, long.MaxValue),
  };

  int p;

  public static Program ParseFile(string file, string source) => new Parser(new Lexer(file, source).Lex(), file).ParseProgram();

  public static bool IsReserved(string word) => Reserved.Contains(word);

  Token Cur => toks[p];

  Token PeekTok(int n = 1) => toks[Math.Min(p + n, toks.Count - 1)];

  Token Next() {
    var t = toks[p];
    if (p < toks.Count - 1) {
      p++;
    }
    return t;
  }

  bool At(T k) => Cur.Kind == k;

  bool AtWord(string w) => Cur.Is(w);

  bool Accept(T k) {
    if (!At(k)) {
      return false;
    }
    Next();
    return true;
  }

  bool AcceptWord(string w) {
    if (!AtWord(w)) {
      return false;
    }
    Next();
    return true;
  }

  CompileError ErrorAt(Token t, string code, string msg) => new(code, msg, file, t.Line, t.Col);

  CompileError Unexpected(string expected) =>
    ErrorAt(Cur, Err.UnexpectedToken, $"expected {expected}, found '{Cur.Text}'");

  CompileError Unsupported(string what) => ErrorAt(Cur, Err.Unsupported, $"{what} is not supported by minimaxon");

  Token Expect(T k, string what) {
    if (!At(k)) {
      throw ErrorAt(Cur, Err.ExpectedToken, $"expected {what}, found '{Cur.Text}'");
    }
    return Next();
  }

  Token ExpectWord(string w) {
    if (!AtWord(w)) {
      throw ErrorAt(Cur, Err.ExpectedToken, $"expected '{w}', found '{Cur.Text}'");
    }
    return Next();
  }

  string ExpectName(string what) {
    if (!At(T.Ident) || Reserved.Contains(Cur.Text)) {
      throw Unexpected(what);
    }
    return Next().Text;
  }

  TNode N<TNode>(TNode node, Token t) where TNode : Node {
    node.File = file;
    node.Line = t.Line;
    node.Col = t.Col;
    return node;
  }

  void SkipNewlines() {
    while (At(T.Newline)) {
      Next();
    }
  }

  void ExpectNewline() {
    if (At(T.Eof)) {
      return;
    }
    Expect(T.Newline, "end of line");
  }

  public Program ParseProgram() {
    var prog = new Program();
    SkipNewlines();
    while (!At(T.Eof)) {
      prog.Decls.Add(ParseTopDecl());
      ExpectNewline();
      SkipNewlines();
    }
    return prog;
  }

  string ParseVisibility() => AtWord("export") || AtWord("public") ? Next().Text : "";

  Decl ParseTopDecl() {
    var vis = ParseVisibility();
    var t = Cur;
    if (AtWord("function")) {
      return ParseFunction(vis, inType: false, isStatic: false);
    }
    if (AtWord("type")) {
      return ParseType(vis);
    }
    if (AtWord("enum")) {
      return ParseEnum(vis);
    }
    if (AtWord("union")) {
      return ParseUnion(vis);
    }
    if (AtWord("typealias")) {
      return ParseAlias(vis);
    }
    if (AtWord("let")) {
      Next();
      var c = N(new ConstDecl { Visibility = vis }, t);
      c.Name = ExpectName("a constant name");
      Expect(T.Assign, "'='");
      c.Value = ParseExpr();
      return c;
    }
    if (AtWord("interface") || AtWord("extension") || AtWord("default") || AtWord("test")) {
      throw Unsupported($"'{Cur.Text}'");
    }
    throw Unexpected("a declaration");
  }

  FuncDecl ParseFunction(string vis, bool inType, bool isStatic) {
    var t = ExpectWord("function");
    var f = N(new FuncDecl { Visibility = vis, IsStatic = isStatic }, t);
    f.Name = ExpectName("a function name");
    Expect(T.LParen, "'('");

    if (!At(T.RParen)) {
      if (At(T.Ident) && PeekTok().Is("self") && Cur.Text is "mutable" or "immutable" or "isolated" or "consuming") {
        if (!inType || isStatic) {
          throw ErrorAt(Cur, Err.UnexpectedToken, "only an instance method takes a receiver parameter");
        }
        f.Receiver = Next().Text;
        Next();
        if (!Accept(T.Comma) && !At(T.RParen)) {
          throw Unexpected("',' or ')'");
        }
      }
      while (!At(T.RParen)) {
        f.Params.Add(ParseParam());
        if (!Accept(T.Comma)) {
          break;
        }
      }
    }
    Expect(T.RParen, "')'");

    if (AcceptWord("returns")) {
      f.Returns = ParseTypeRef();
    }
    if (AtWord("throws")) {
      throw Unsupported("'throws'");
    }

    ExpectNewline();
    f.Body = ParseBlock(t);
    ExpectEnd(f.Name);
    return f;
  }

  Param ParseParam() {
    var t = Cur;
    var prm = N(new Param(), t);
    if (!At(T.Ident) || (Reserved.Contains(Cur.Text) && Cur.Text != "_")) {
      throw Unexpected("a parameter name");
    }
    prm.Name = Next().Text;
    if (AtWord("mutable") && PeekTok().Kind == T.Ident) {
      Next();
      prm.Mutable = true;
    }
    prm.Type = ParseTypeRef();
    if (At(T.Assign)) {
      throw Unsupported("a default parameter value");
    }
    return prm;
  }

  TypeRef ParseTypeRef() {
    var t = Cur;
    var tr = N(new TypeRef(), t);
    if (At(T.Ident) && Capabilities.Contains(Cur.Text) && PeekTok().Kind == T.Ident) {
      tr.Capability = Next().Text;
    }
    if (At(T.LParen)) {
      throw Unsupported("a tuple type");
    }
    if (!At(T.Ident)) {
      throw ErrorAt(Cur, Err.ExpectedType, $"expected a type, found '{Cur.Text}'");
    }
    tr.Name = Next().Text;
    while (Accept(T.Dot)) {
      tr.Qualifier.Add(tr.Name);
      tr.Name = ExpectName("a type name");
    }
    if (AtWord("with")) {
      throw Unsupported("a generic type");
    }
    return tr;
  }

  void ExpectEnd(string label) {
    ExpectWord("end");
    if (At(T.Quote)) {
      var q = Next();
      if (q.Text != label) {
        throw ErrorAt(q, Err.MismatchedEndLabel, $"'end' names '{q.Text}', but the block is '{label}'");
      }
    }
  }

  string ExpectLabel() => Expect(T.Quote, "a block label").Text;

  List<Stmt> ParseBlock(Token opener) {
    var list = new List<Stmt>();
    SkipNewlines();
    while (!AtWord("end")) {
      if (At(T.Eof)) {
        throw Unexpected("'end'");
      }
      list.Add(ParseStatement());
      if (AtWord("end")) {
        break;
      }
      ExpectNewline();
      SkipNewlines();
    }
    if (list.Count == 0) {
      throw ErrorAt(opener, Err.EmptyBlock, "a block must contain at least one statement");
    }
    return list;
  }

  TypeDecl ParseType(string vis) {
    var t = ExpectWord("type");
    var d = N(new TypeDecl { Visibility = vis }, t);
    d.Name = ExpectName("a type name");
    if (AtWord("is") || AtWord("uses") || AtWord("implements") || AtWord("where")) {
      throw Unsupported($"'{Cur.Text}' on a type");
    }
    ExpectNewline();
    SkipNewlines();

    while (!AtWord("end")) {
      var mvis = ParseVisibility();
      var mt = Cur;
      if (AtWord("var") || AtWord("let")) {
        var fd = N(new FieldDecl { Visibility = mvis, IsVar = Next().Text == "var" }, mt);
        fd.Name = ExpectName("a field name");
        if (AcceptWord("as")) {
          fd.Type = ParseTypeRef();
        } else if (Accept(T.Assign)) {
          fd.Default = ParseExpr();
        } else {
          throw Unexpected("'as' or '='");
        }
        d.Fields.Add(fd);
      } else if (AtWord("function")) {
        d.Methods.Add(ParseFunction(mvis, inType: true, isStatic: false));
      } else if (AtWord("static")) {
        Next();
        if (!AtWord("function")) {
          throw Unsupported("a static field");
        }
        d.Methods.Add(ParseFunction(mvis, inType: true, isStatic: true));
      } else {
        throw Unexpected("a field or method");
      }
      ExpectNewline();
      SkipNewlines();
    }
    ExpectEnd(d.Name);
    return d;
  }

  EnumDecl ParseEnum(string vis) {
    var t = ExpectWord("enum");
    var d = N(new EnumDecl { Visibility = vis }, t);
    d.Name = ExpectName("an enum name");
    if (AtWord("implements")) {
      throw Unsupported("'implements' on an enum");
    }
    ExpectNewline();
    SkipNewlines();

    while (!AtWord("end")) {
      var mvis = ParseVisibility();
      if (AtWord("function")) {
        d.Methods.Add(ParseFunction(mvis, inType: true, isStatic: false));
      } else {
        var ct = Cur;
        var c = N(new EnumCaseDecl { Name = ExpectName("an enum case") }, ct);
        if (Accept(T.Assign)) {
          var neg = Accept(T.Minus);
          if (!At(T.Int)) {
            throw Unsupported("a non-integer enum raw value");
          }
          var v = Next().IntValue;
          c.Raw = neg ? -v : v;
        }
        d.Cases.Add(c);
      }
      ExpectNewline();
      SkipNewlines();
    }
    ExpectEnd(d.Name);
    return d;
  }

  UnionDecl ParseUnion(string vis) {
    var t = ExpectWord("union");
    var d = N(new UnionDecl { Visibility = vis }, t);
    d.Name = ExpectName("a union name");
    if (AtWord("implements")) {
      throw Unsupported("'implements' on a union");
    }
    ExpectNewline();
    SkipNewlines();

    while (!AtWord("end")) {
      var mvis = ParseVisibility();
      if (AtWord("function")) {
        d.Methods.Add(ParseFunction(mvis, inType: true, isStatic: false));
      } else {
        var ct = Cur;
        var c = N(new UnionCaseDecl { Name = ExpectName("a union case") }, ct);
        if (Accept(T.LParen)) {
          while (!At(T.RParen)) {
            var fp = ParseParam();
            if (fp.Mutable) {
              throw ErrorAt(ct, Err.ExpectedToken, "a union payload takes no 'mutable'");
            }
            c.Fields.Add(fp);
            if (!Accept(T.Comma)) {
              break;
            }
          }
          Expect(T.RParen, "')'");
        }
        if (At(T.Assign)) {
          throw Unsupported("a union raw value");
        }
        d.Cases.Add(c);
      }
      ExpectNewline();
      SkipNewlines();
    }
    ExpectEnd(d.Name);
    return d;
  }

  AliasDecl ParseAlias(string vis) {
    var t = ExpectWord("typealias");
    var d = N(new AliasDecl { Visibility = vis }, t);
    d.Name = ExpectName("an alias name");
    Expect(T.Assign, "'='");
    if (!AtWord("int")) {
      throw Unsupported("a typealias other than a ranged int");
    }
    Next();
    Expect(T.LParen, "'('");
    d.Min = ParseBound();
    var inclusive = AtWord("to");
    if (!AcceptWord("to") && !AcceptWord("upto")) {
      throw Unexpected("'to' or 'upto'");
    }
    d.Max = ParseBound();
    if (!inclusive) {
      d.Max -= 1;
    }
    Expect(T.RParen, "')'");
    if (AtWord("implements")) {
      throw Unsupported("'implements' on a typealias");
    }
    if (d.Min > d.Max) {
      throw d.Error(Err.TypeMismatch, $"the range of '{d.Name}' is empty");
    }
    if (d.Min < 0 && d.Max > IntTy.I64Max) {
      throw d.Error(Err.TypeMismatch, "an integer range cannot span both negative values and values above i64.max");
    }
    return d;
  }

  Int128 ParseBound() {
    var neg = Accept(T.Minus);
    Int128 v;
    if (At(T.Int)) {
      v = Next().IntValue;
    } else if (At(T.Ident) && SizedTypes.TryGetValue(Cur.Text, out var range)) {
      Next();
      Expect(T.Dot, "'.'");
      var which = ExpectName("'min' or 'max'");
      v = which switch {
        "min" => range.Min,
        "max" => range.Max,
        _ => throw Unexpected("'min' or 'max'"),
      };
    } else {
      throw Unexpected("a range bound");
    }
    return neg ? -v : v;
  }

  Stmt ParseStatement() {
    var t = Cur;

    if (AtWord("let") || AtWord("var")) {
      var s = N(new LetStmt { IsVar = Next().Text == "var" }, t);
      if (At(T.LParen)) {
        throw Unsupported("tuple destructuring");
      }
      s.Name = ExpectName("a variable name");
      Expect(T.Assign, "'='");
      s.Init = ParseExpr();
      return s;
    }

    if (At(T.Ident) && Cur.Text == "_" && PeekTok().Kind == T.Assign) {
      Next();
      Next();
      return N(new DiscardStmt { Value = ParseExpr() }, t);
    }

    if (AtWord("if")) {
      return ParseIf();
    }

    if (AtWord("while")) {
      Next();
      var s = N(new WhileStmt { Cond = ParseExpr() }, t);
      s.Label = ExpectLabel();
      s.Body = ParseBlock(t);
      ExpectEnd(s.Label);
      return s;
    }

    if (AtWord("for")) {
      Next();
      var s = N(new ForStmt(), t);
      if (At(T.LParen)) {
        throw Unsupported("a destructuring for loop");
      }
      if (!At(T.Ident) || (Reserved.Contains(Cur.Text) && Cur.Text != "_")) {
        throw Unexpected("a loop variable");
      }
      s.Var = Next().Text;
      ExpectWord("in");
      s.From = ParseOr();
      s.Inclusive = AtWord("to");
      if (!AcceptWord("to") && !AcceptWord("upto")) {
        throw Unsupported("a for loop over anything but a range");
      }
      s.To = ParseOr();
      s.Label = ExpectLabel();
      s.Body = ParseBlock(t);
      ExpectEnd(s.Label);
      return s;
    }

    if (AtWord("match")) {
      return N(new MatchStmt { Match = ParseMatchCore() }, t);
    }

    if (AtWord("return")) {
      Next();
      var s = N(new ReturnStmt(), t);
      if (!At(T.Newline) && !At(T.Eof) && !AtWord("end")) {
        s.Value = ParseExpr();
      }
      return s;
    }

    if (AtWord("break") || AtWord("continue")) {
      var isBreak = Next().Text == "break";
      string? label = At(T.Quote) ? Next().Text : null;
      return isBreak ? N(new BreakStmt { Label = label }, t) : N(new ContinueStmt { Label = label }, t);
    }

    if (At(T.At)) {
      throw Unsupported("'@heap'");
    }
    if (AtWord("try") || AtWord("throw")) {
      throw Unsupported($"'{Cur.Text}'");
    }
    if ((AtWord("modify") || AtWord("acquire")) && PeekTok().Kind == T.Ident) {
      throw Unsupported($"'{Cur.Text}'");
    }

    var e = ParseExpr();
    if (Accept(T.Assign)) {
      if (e is not (NameExpr or MemberExpr)) {
        throw ErrorAt(t, Err.UnexpectedToken, "only a variable or a field can be assigned");
      }
      return N(new AssignStmt { Target = e, Value = ParseExpr() }, t);
    }
    if (e is not (CallExpr or PanicExpr)) {
      throw ErrorAt(t, Err.UnexpectedToken, "only a call can stand alone as a statement");
    }
    return N(new ExprStmt { E = e }, t);
  }

  IfStmt ParseIf() {
    var t = ExpectWord("if");
    if (AtWord("let") || AtWord("var") || AtWord("try")) {
      throw Unsupported("'if let' and 'if try'");
    }
    var s = N(new IfStmt { Cond = ParseExpr() }, t);
    s.Label = ExpectLabel();
    s.Then = ParseBlock(t);
    ExpectEnd(s.Label);

    if (AtWord("else")) {
      var et = Next();
      if (AtWord("if")) {
        s.Else = [ParseIf()];
      } else {
        if (At(T.LParen)) {
          throw Unsupported("an else binding");
        }
        var label = ExpectLabel();
        s.Else = ParseBlock(et);
        ExpectEnd(label);
      }
    }
    return s;
  }

  MatchCore ParseMatchCore() {
    var t = ExpectWord("match");
    var m = N(new MatchCore { Subject = ParseExpr() }, t);
    m.Label = ExpectLabel();
    ExpectNewline();
    SkipNewlines();
    while (!AtWord("end")) {
      m.Arms.Add(ParseArm());
      ExpectNewline();
      SkipNewlines();
    }
    ExpectEnd(m.Label);
    return m;
  }

  MatchArm ParseArm() {
    var t = Cur;
    var arm = N(new MatchArm(), t);
    if (AcceptWord("default")) {
      arm.IsDefault = true;
    } else {
      while (true) {
        arm.Patterns.Add(ParsePattern());
        if (AtWord("or") && PeekTok().Kind == T.Newline) {
          Next();
          SkipNewlines();
          continue;
        }
        break;
      }
    }

    if (AcceptWord("then")) {
      arm.Kind = ArmKind.Then;
      arm.Action = ParseArmStatement();
      if (AtWord("and")) {
        throw Unsupported("'and fallthrough'");
      }
    } else if (AcceptWord("gives")) {
      arm.Kind = ArmKind.Gives;
      arm.Value = ParseExpr();
    } else if (AtWord("panic")) {
      arm.Kind = ArmKind.Panic;
      arm.Value = ParsePrimary();
    } else if (AtWord("throws")) {
      throw Unsupported("'throws' in a match arm");
    } else {
      throw Unexpected("'then', 'gives' or 'panic'");
    }
    return arm;
  }

  Stmt ParseArmStatement() {
    if (AtWord("if") || AtWord("while") || AtWord("for") || AtWord("match")) {
      throw ErrorAt(Cur, Err.MatchBlockStatement, $"a '{Cur.Text}' block cannot be a match arm's action");
    }
    return ParseStatement();
  }

  Pattern ParsePattern() {
    var t = Cur;
    var pat = N(new Pattern(), t);
    if (At(T.Ident) && !AtWord("true") && !AtWord("false")) {
      pat.Kind = PatternKind.Case;
      pat.CaseName = ExpectName("a case name");
      if (At(T.Dot)) {
        throw ErrorAt(t, "E3075", "a match arm names a bare case, without its type");
      }
      if (Accept(T.LParen)) {
        pat.Bindings = [];
        while (!At(T.RParen)) {
          if (!At(T.Ident) || (Reserved.Contains(Cur.Text) && Cur.Text != "_")) {
            throw Unexpected("a binding name");
          }
          pat.Bindings.Add(Next().Text);
          if (!Accept(T.Comma)) {
            break;
          }
        }
        Expect(T.RParen, "')'");
      }
      return pat;
    }

    pat.Value = ParsePatternValue();
    if (AtWord("to") || AtWord("upto")) {
      pat.Kind = PatternKind.Range;
      pat.Inclusive = Next().Text == "to";
      pat.High = ParsePatternValue();
    } else {
      pat.Kind = PatternKind.Literal;
    }
    return pat;
  }

  Expr ParsePatternValue() {
    var t = Cur;
    if (Accept(T.Minus)) {
      var v = Expect(T.Int, "an integer").IntValue;
      return N(new IntLit { Value = -v }, t);
    }
    if (At(T.Int) || At(T.Str) || AtWord("true") || AtWord("false")) {
      return ParsePrimary();
    }
    throw Unexpected("a pattern");
  }

  Expr ParseStandaloneExpr() {
    var e = ParseExpr();
    if (!At(T.Newline) && !At(T.Eof)) {
      throw Unexpected("the end of the interpolation");
    }
    return e;
  }

  public Expr ParseExpr() {
    var t = Cur;
    var e = ParseOr();
    if (AtWord("if")) {
      Next();
      var cond = ParseOr();
      ExpectWord("else");
      var els = ParseExpr();
      return N(new CondExpr { Cond = cond, Then = e, Else = els }, t);
    }
    if (AtWord("to") || AtWord("upto")) {
      throw Unsupported("a range value");
    }
    return e;
  }

  static readonly string[][] BinaryLevels = [
    ["or"],
    ["xor"],
    ["and"],
    ["==", "!=", "<", "<=", ">", ">="],
    ["shl", "shr"],
    ["+", "-"],
    ["*", "/", "mod"],
  ];

  const int ComparisonLevel = 3;

  string? BinaryOperator(int level) {
    var t = Cur;
    if (t.Kind is T.Str or T.Quote or T.Int) {
      return null;
    }
    foreach (var op in BinaryLevels[level]) {
      if (t.Text == op) {
        return op;
      }
    }
    return null;
  }

  Expr ParseBinary(int level) {
    if (level == BinaryLevels.Length) {
      return ParseCast();
    }
    var e = ParseBinary(level + 1);
    while (true) {
      if (level == ComparisonLevel && AtWord("is")) {
        throw Unsupported("'is'");
      }
      var t = Cur;
      if (BinaryOperator(level) is not { } op) {
        return e;
      }
      Next();
      e = N(new BinaryExpr { Op = op, L = e, R = ParseBinary(level + 1) }, t);
    }
  }

  Expr ParseOr() => ParseBinary(0);

  Expr ParseCast() {
    var t = Cur;
    var e = ParseUnary();
    if (AtWord("as")) {
      Next();
      var target = ParseTypeRef();
      e = N(new CastExpr { E = e, Target = target }, t);
      if (AtWord("as")) {
        throw ErrorAt(Cur, "E2079", "a cast of a cast: cast once, or bind the first cast to a 'let'");
      }
    }
    return e;
  }

  Expr ParseUnary() {
    var t = Cur;
    if (Accept(T.Minus)) {
      if (At(T.Minus)) {
        throw ErrorAt(Cur, Err.UnexpectedToken, "'--x' is not an expression; write '-(-x)'");
      }
      if (At(T.Int) && PeekTok().Kind != T.Dot) {
        var lit = Next();
        return N(new IntLit { Value = -lit.IntValue }, t);
      }
      return N(new UnaryExpr { Op = "-", E = ParsePostfix() }, t);
    }
    if (AcceptWord("not")) {
      return N(new UnaryExpr { Op = "not", E = ParseUnary() }, t);
    }
    if (AtWord("async") || AtWord("await")) {
      throw Unsupported($"'{Cur.Text}'");
    }
    if ((AtWord("freeze") || AtWord("merge") || AtWord("swap") || AtWord("spawn")) &&
        PeekTok().Kind == T.Ident) {
      throw Unsupported($"'{Cur.Text}'");
    }
    return ParsePostfix();
  }

  Expr ParsePostfix() {
    var e = ParsePrimary();
    while (true) {
      var t = Cur;
      if (Accept(T.Dot)) {
        if (At(T.Int)) {
          throw Unsupported("tuple element access");
        }
        if (!At(T.Ident)) {
          throw Unexpected("a member name");
        }
        var name = Next().Text;
        e = N(new MemberExpr { Obj = e, Name = name }, t);
      } else if (At(T.LParen)) {
        e = N(new CallExpr { Callee = e, Args = ParseArgs() }, t);
      } else {
        return e;
      }
    }
  }

  List<Arg> ParseArgs() {
    Expect(T.LParen, "'('");
    var args = new List<Arg>();
    while (!At(T.RParen)) {
      var t = Cur;
      var arg = N(new Arg(), t);
      if (At(T.Ident) && PeekTok().Kind == T.Colon) {
        arg.Label = Next().Text;
        Next();
      }
      arg.Value = ParseExpr();
      args.Add(arg);
      if (!Accept(T.Comma)) {
        break;
      }
    }
    Expect(T.RParen, "')'");
    return args;
  }

  Expr ParsePrimary() {
    var t = Cur;

    if (At(T.Int)) {
      return N(new IntLit { Value = Next().IntValue }, t);
    }

    if (At(T.Str)) {
      var tok = Next();
      var lit = N(new StrLit(), t);
      foreach (var part in tok.Parts!) {
        if (part is TextPart tp) {
          lit.Parts.Add(tp.Text);
        } else if (part is ExprPart ep) {
          var sub = new Lexer(file, ep.Source, ep.Line, ep.Col).Lex();
          lit.Parts.Add(new Parser(sub, file).ParseStandaloneExpr());
        }
      }
      return lit;
    }

    if (AtWord("true") || AtWord("false")) {
      return N(new BoolLit { Value = Next().Text == "true" }, t);
    }

    if (At(T.Ident) && PeekTok().Kind == T.LBrace && (AtWord("Self") || !Reserved.Contains(Cur.Text))) {
      var s = N(new StructLitExpr { TypeName = Next().Text }, t);
      Expect(T.LBrace, "'{'");
      while (!At(T.RBrace)) {
        var ft = Cur;
        var name = ExpectName("a field name");
        Expect(T.Colon, "':'");
        s.Fields.Add((name, ParseExpr(), N(new Arg(), ft)));
        if (!Accept(T.Comma)) {
          break;
        }
      }
      Expect(T.RBrace, "'}'");
      return s;
    }

    if (AcceptWord("self")) {
      return N(new SelfExpr(), t);
    }

    if (AcceptWord("Self")) {
      return N(new NameExpr { Name = "Self" }, t);
    }

    if (AtWord("panic")) {
      Next();
      Expect(T.LParen, "'('");
      var msg = ParseExpr();
      Expect(T.RParen, "')'");
      return N(new PanicExpr { Message = msg }, t);
    }

    if (AtWord("match")) {
      return N(new MatchExpr { Match = ParseMatchCore() }, t);
    }

    if (Accept(T.LParen)) {
      var e = ParseExpr();
      if (At(T.Comma)) {
        throw Unsupported("a tuple");
      }
      Expect(T.RParen, "')'");
      return e;
    }

    if (At(T.LBracket)) {
      throw Unsupported("an array literal");
    }
    if (AtWord("function")) {
      throw Unsupported("a closure");
    }
    if (AtWord("try")) {
      throw Unsupported("'try'");
    }

    if (At(T.Ident) && !Reserved.Contains(Cur.Text)) {
      return N(new NameExpr { Name = Next().Text }, t);
    }

    throw Unexpected("an expression");
  }
}
